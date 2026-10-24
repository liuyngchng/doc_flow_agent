package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strings"

	"doc_flow_agent/internal/model"
	"doc_flow_agent/internal/store"

	"github.com/gin-gonic/gin"
)

// 审稿模拟支持的文件扩展名
var proofreadAllowedExts = map[string]bool{
	".docx": true,
	".doc":  true,
	".pdf":  true,
	".txt":  true,
	".md":   true,
	".xlsx": true,
}

// ProofreadHandler 审稿模拟处理器（代理外部 rdCLI 审稿服务，通过回调接收结果）
type ProofreadHandler struct {
	cfg   *model.Config
	store store.MetaStore
}

// NewProofreadHandler 创建审稿模拟处理器
func NewProofreadHandler(cfg *model.Config, st store.MetaStore) *ProofreadHandler {
	return &ProofreadHandler{cfg: cfg, store: st}
}

// Submit 提交审稿任务到外部 rdCLI，附带回调地址
// POST /api/inner/proofread/submit
func (h *ProofreadHandler) Submit(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(400, gin.H{"status": "error", "error": "请上传文件"})
		return
	}

	// 校验文件扩展名
	ext := strings.ToLower(filepath.Ext(file.Filename))
	if !proofreadAllowedExts[ext] {
		slog.Warn("proofread_unsupported_format", "user", userName(c), "filename", file.Filename, "ext", ext)
		c.JSON(400, gin.H{
			"status": "error",
			"error":  fmt.Sprintf("不支持的文件格式: %s，仅支持 docx/doc/pdf/txt/md/xlsx", ext),
		})
		return
	}

	// 读取外部服务配置
	baseURL, apiKey, err := h.getProofreadConfig()
	if err != nil {
		c.JSON(400, gin.H{"status": "error", "error": err.Error()})
		return
	}

	// 构造回调地址
	callbackURL := h.buildCallbackURL()
	if callbackURL == "" {
		c.JSON(400, gin.H{"status": "error", "error": "未配置 self_url，请在 cfg.yml 中设置 sys.self_url"})
		return
	}

	// 打开上传文件
	src, err := file.Open()
	if err != nil {
		c.JSON(500, gin.H{"status": "error", "error": "读取文件失败"})
		return
	}
	defer src.Close()

	instructions := strings.TrimSpace(c.PostForm("additional_instructions"))

	// 构造 multipart 请求到 rdCLI（附带 callback_url）
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", file.Filename)
	if err != nil {
		c.JSON(500, gin.H{"status": "error", "error": "构建请求失败"})
		return
	}
	if _, err := io.Copy(part, src); err != nil {
		c.JSON(500, gin.H{"status": "error", "error": "读取文件数据失败"})
		return
	}
	writer.WriteField("callback_url", callbackURL)
	if instructions != "" {
		writer.WriteField("additional_instructions", instructions)
	}
	writer.Close()

	reqURL := baseURL
	req, err := http.NewRequest(http.MethodPost, reqURL, &body)
	if err != nil {
		c.JSON(500, gin.H{"status": "error", "error": "创建代理请求失败"})
		return
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if apiKey != "" {
		req.Header.Set("x-api-key", apiKey)
	}

	slog.Info("proofread_forward", "url", reqURL, "user", userName(c), "filename", file.Filename, "ext", ext, "callback_url", callbackURL)

	resp, err := docParserClient.Do(req)
	if err != nil {
		slog.Error("proofread_submit_failed", "user", userName(c), "error", err)
		c.JSON(502, gin.H{"status": "error", "error": "审稿服务请求失败: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		c.JSON(502, gin.H{"status": "error", "error": "读取审稿服务响应失败"})
		return
	}

	slog.Info("proofread_submit_response", "user", userName(c), "filename", file.Filename, "rdcli_status", resp.StatusCode)

	// rdCLI 返回非 200 时向前端报错
	if resp.StatusCode != 200 {
		errDetail := strings.TrimSpace(string(respBody))
		if errDetail == "" {
			errDetail = fmt.Sprintf("外部服务返回状态码 %d", resp.StatusCode)
		} else if !json.Valid([]byte(errDetail)) {
			if len(errDetail) > 200 {
				errDetail = errDetail[:200] + "..."
			}
		}
		slog.Warn("proofread_upstream_error", "url", reqURL, "status", resp.StatusCode, "body", errDetail)
		c.JSON(502, gin.H{"status": "error", "error": fmt.Sprintf("审稿服务异常 (status=%d): %s", resp.StatusCode, errDetail)})
		return
	}

	// 从 rdCLI 响应中提取 task_id（按 API 文档格式：task_id 在顶层）
	// 响应示例: {"code":0, "task_id":"550e8400-...", "data":{"status":"pending"}}
	var rdcliResp struct {
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal(respBody, &rdcliResp); err != nil || rdcliResp.TaskID == "" {
		slog.Warn("proofread_parse_task_id_failed", "user", userName(c), "error", err, "body", ellipsisJSON(string(respBody), 500))
		c.JSON(502, gin.H{"status": "error", "error": "审稿服务返回异常，无法获取任务 ID"})
		return
	}

	taskID := rdcliResp.TaskID

	// 记录 rdCLI 生成的 task_id，后续回调时用来匹配
	h.store.SetConfig("proofread_current_task_id", taskID, "审稿任务 ID")
	h.store.SetConfig("proofread_current_status", "pending", "审稿任务状态")
	h.store.SetConfig("proofread_current_file_name", file.Filename, "审稿文件名")
	h.store.SetConfig("proofread_current_result", "", "审稿结果（等待回调）")

	slog.Info("proofread_task_created", "user", userName(c), "task_id", taskID, "filename", file.Filename)

	// 返回 rdCLI 的 task_id 给前端
	c.JSON(200, gin.H{
		"success": true,
		"data":    gin.H{"task_id": taskID},
	})
}

// Callback 接收 rdCLI 核稿完成后的回调推送
// POST /api/inner/proofread/callback
// 此接口由 rdCLI 调用，不需要 Cookie 认证
//
// 回调 body 格式（按外部 API 文档）：
//
//	{"code":0, "task_id":"550e8400-...", "data":{"status":"completed", "file_name":"...", "result_text":"..."}}
//
// task_id 在顶层，与 Submit 时 rdCLI 返回的一致。
// 拿它跟 DB 里记录的 proofread_current_task_id 对比，匹配则更新结果。
func (h *ProofreadHandler) Callback(c *gin.Context) {
	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		slog.Warn("proofread_callback_read_failed", "error", err)
		c.JSON(400, gin.H{"status": "error", "error": "读取请求体失败"})
		return
	}

	slog.Info("proofread_callback_received", "body", ellipsisJSON(string(bodyBytes), 2000))

	// 解析 rdCLI 回调 body（task_id 在顶层，status/file_name 在 data 里）
	var parsed struct {
		TaskID string `json:"task_id"`
		Data   struct {
			Status   string `json:"status"`
			FileName string `json:"file_name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(bodyBytes, &parsed); err != nil {
		slog.Warn("proofread_callback_parse_failed", "error", err)
		c.JSON(400, gin.H{"status": "error", "error": "请求体不是合法 JSON"})
		return
	}

	if parsed.TaskID == "" {
		c.JSON(400, gin.H{"status": "error", "error": "缺少 task_id"})
		return
	}

	// 跟 DB 里记录的 task_id 对比
	storedTaskID, _ := h.store.GetConfig("proofread_current_task_id")
	if storedTaskID != "" && storedTaskID != parsed.TaskID {
		slog.Warn("proofread_callback_mismatch",
			"callback_task_id", parsed.TaskID,
			"stored_task_id", storedTaskID)
		// task_id 不匹配 — 可能是旧任务的回调，忽略
		c.JSON(200, gin.H{"status": "ok", "ignored": true})
		return
	}

	// 匹配成功，更新结果
	status := parsed.Data.Status
	h.store.SetConfig("proofread_current_status", status, "审稿任务状态")
	h.store.SetConfig("proofread_current_result", string(bodyBytes), "审稿结果 JSON")
	if parsed.Data.FileName != "" {
		h.store.SetConfig("proofread_current_file_name", parsed.Data.FileName, "审稿文件名")
	}

	slog.Info("proofread_callback_stored", "task_id", parsed.TaskID, "status", status)

	// 返回 200 确认接收（rdCLI 要求 2xx）
	c.JSON(200, gin.H{"status": "ok"})
}

// Query 查询审稿结果 — 从本地 DB 读取
// GET /api/inner/proofread/query/:task_id
func (h *ProofreadHandler) Query(c *gin.Context) {
	taskID := c.Param("task_id")
	if taskID == "" {
		c.JSON(400, gin.H{"status": "error", "error": "缺少任务 ID"})
		return
	}

	// 校验 task_id 是否匹配当前存储的任务
	storedTaskID, _ := h.store.GetConfig("proofread_current_task_id")

	slog.Info("proofread_query",
		"user", userName(c),
		"query_task_id", taskID,
		"stored_task_id", storedTaskID,
		"match", storedTaskID == taskID,
	)

	if storedTaskID != taskID {
		slog.Warn("proofread_query_task_mismatch",
			"user", userName(c),
			"query_task_id", taskID,
			"stored_task_id", storedTaskID,
		)
		c.JSON(404, gin.H{"status": "error", "error": "任务不存在或已过期"})
		return
	}

	result, _ := h.store.GetConfig("proofread_current_result")

	if result == "" {
		// 还没有收到回调，返回进行中状态
		status, _ := h.store.GetConfig("proofread_current_status")
		fileName, _ := h.store.GetConfig("proofread_current_file_name")

		slog.Info("proofread_query_no_result_yet",
			"user", userName(c),
			"task_id", taskID,
			"status", status,
			"file_name", fileName,
		)

		c.JSON(200, gin.H{
			"success": true,
			"data": gin.H{
				"task_id":   taskID,
				"status":    status,
				"file_name": fileName,
			},
		})
		return
	}

	// 已经有回调结果，直接返回
	var resultJSON interface{}
	if err := json.Unmarshal([]byte(result), &resultJSON); err != nil {
		slog.Error("proofread_query_result_corrupted",
			"user", userName(c),
			"task_id", taskID,
			"error", err,
		)
		c.JSON(500, gin.H{"status": "error", "error": "结果数据损坏"})
		return
	}

	slog.Info("proofread_query_result",
		"user", userName(c),
		"task_id", taskID,
	)

	c.JSON(200, gin.H{"success": true, "data": resultJSON})
}

// State 返回当前审稿任务状态（页面加载后恢复用）
// GET /api/inner/proofread/state
func (h *ProofreadHandler) State(c *gin.Context) {
	taskID, _ := h.store.GetConfig("proofread_current_task_id")
	status, _ := h.store.GetConfig("proofread_current_status")
	fileName, _ := h.store.GetConfig("proofread_current_file_name")

	c.JSON(200, gin.H{
		"status": "ok",
		"data": gin.H{
			"task_id":   taskID,
			"status":    status,
			"file_name": fileName,
		},
	})
}

// getProofreadConfig 从 yaml 配置读取审稿服务配置
func (h *ProofreadHandler) getProofreadConfig() (string, string, error) {
	baseURL := strings.TrimRight(h.cfg.DocAudit.APIURL, "/")
	if baseURL == "" {
		return "", "", fmt.Errorf("未配置文档审核服务地址，请先在 cfg.yml 中配置 doc_audit.api_url")
	}
	return baseURL, h.cfg.DocAudit.Token, nil
}

// buildCallbackURL 构造 rdCLI 回调地址
func (h *ProofreadHandler) buildCallbackURL() string {
	selfURL := strings.TrimRight(h.cfg.Sys.SelfURL, "/")
	if selfURL == "" {
		return ""
	}
	return selfURL + "/api/inner/proofread/callback"
}