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

	req, err := http.NewRequest(http.MethodPost, baseURL+"/api/proofread", &body)
	if err != nil {
		c.JSON(500, gin.H{"status": "error", "error": "创建代理请求失败"})
		return
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if apiKey != "" {
		req.Header.Set("x-api-key", apiKey)
	}

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

	slog.Info("proofread_submit", "user", userName(c), "filename", file.Filename, "ext", ext, "status", resp.StatusCode, "callback_url", callbackURL)

	// 从 rdCLI 响应中提取 task_id 并持久化
	if resp.StatusCode == 200 {
		var parsed struct {
			Success bool `json:"success"`
			Data    struct {
				TaskID string `json:"task_id"`
			} `json:"data"`
		}
		if json.Unmarshal(respBody, &parsed) == nil && parsed.Success && parsed.Data.TaskID != "" {
			h.store.SetConfig("proofread_current_task_id", parsed.Data.TaskID, "审稿任务 ID")
			h.store.SetConfig("proofread_current_status", "pending", "审稿任务状态")
			h.store.SetConfig("proofread_current_file_name", file.Filename, "审稿文件名")
			h.store.SetConfig("proofread_current_result", "", "审稿结果（等待回调）")
		}
	}

	c.Data(resp.StatusCode, "application/json", respBody)
}

// Callback 接收 rdCLI 核稿完成后的回调推送
// POST /api/inner/proofread/callback
// 此接口由 rdCLI 调用，不需要 Cookie 认证
func (h *ProofreadHandler) Callback(c *gin.Context) {
	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		slog.Warn("proofread_callback_read_failed", "error", err)
		c.JSON(400, gin.H{"status": "error", "error": "读取请求体失败"})
		return
	}

	slog.Info("proofread_callback_received", "body", ellipsisJSON(string(bodyBytes), 2000))

	// 解析回调内容
	var parsed struct {
		TaskID   string `json:"task_id"`
		Status   string `json:"status"`
		FileName string `json:"file_name"`
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

	// 持久化结果
	h.store.SetConfig("proofread_current_task_id", parsed.TaskID, "审稿任务 ID")
	h.store.SetConfig("proofread_current_status", parsed.Status, "审稿任务状态")
	h.store.SetConfig("proofread_current_result", string(bodyBytes), "审稿结果 JSON")

	if parsed.FileName != "" {
		h.store.SetConfig("proofread_current_file_name", parsed.FileName, "审稿文件名")
	}

	slog.Info("proofread_callback_stored", "task_id", parsed.TaskID, "status", parsed.Status)

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
	if storedTaskID != taskID {
		c.JSON(404, gin.H{"status": "error", "error": "任务不存在或已过期"})
		return
	}

	result, _ := h.store.GetConfig("proofread_current_result")

	if result == "" {
		// 还没有收到回调，返回进行中状态
		status, _ := h.store.GetConfig("proofread_current_status")
		fileName, _ := h.store.GetConfig("proofread_current_file_name")
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
		c.JSON(500, gin.H{"status": "error", "error": "结果数据损坏"})
		return
	}

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