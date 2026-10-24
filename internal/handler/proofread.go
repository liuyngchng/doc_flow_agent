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

// ProofreadHandler 审稿模拟处理器（代理外部 rdCLI 审稿服务）
type ProofreadHandler struct {
	cfg   *model.Config
	store store.MetaStore
}

// NewProofreadHandler 创建审稿模拟处理器
func NewProofreadHandler(cfg *model.Config, st store.MetaStore) *ProofreadHandler {
	return &ProofreadHandler{cfg: cfg, store: st}
}

// submitInternal 提交审稿任务到外部 rdCLI（内部复用）
func (h *ProofreadHandler) submitInternal(baseURL, apiKey string, src io.Reader, filename string, instructions string) (*http.Response, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return nil, fmt.Errorf("构建请求失败: %w", err)
	}
	if _, err := io.Copy(part, src); err != nil {
		return nil, fmt.Errorf("读取文件数据失败: %w", err)
	}
	if instructions != "" {
		writer.WriteField("additional_instructions", instructions)
	}
	writer.Close()

	req, err := http.NewRequest(http.MethodPost, baseURL+"/api/proofread", &body)
	if err != nil {
		return nil, fmt.Errorf("创建代理请求失败: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if apiKey != "" {
		req.Header.Set("x-api-key", apiKey)
	}

	return docParserClient.Do(req)
}

// queryInternal 查询审稿结果（内部复用）
func (h *ProofreadHandler) queryInternal(baseURL, apiKey, taskID string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, baseURL+"/api/proofread/"+taskID, nil)
	if err != nil {
		return nil, fmt.Errorf("创建代理请求失败: %w", err)
	}
	if apiKey != "" {
		req.Header.Set("x-api-key", apiKey)
	}
	return docParserClient.Do(req)
}

// Submit 提交审稿任务 — 代理 multipart 到外部 rdCLI
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

	// 打开上传文件
	src, err := file.Open()
	if err != nil {
		c.JSON(500, gin.H{"status": "error", "error": "读取文件失败"})
		return
	}
	defer src.Close()

	instructions := strings.TrimSpace(c.PostForm("additional_instructions"))

	resp, err := h.submitInternal(baseURL, apiKey, src, file.Filename, instructions)
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

	// 从 rdCLI 响应中提取 task_id 并持久化
	h.persistFromSubmit(respBody)

	slog.Info("proofread_submit", "user", userName(c), "filename", file.Filename, "ext", ext, "status", resp.StatusCode)

	c.Data(resp.StatusCode, "application/json", respBody)
}

// Query 查询审稿结果 — 代理 GET 到外部 rdCLI，并更新持久化状态
// GET /api/inner/proofread/query/:task_id
func (h *ProofreadHandler) Query(c *gin.Context) {
	taskID := c.Param("task_id")
	if taskID == "" {
		c.JSON(400, gin.H{"status": "error", "error": "缺少任务 ID"})
		return
	}

	baseURL, apiKey, err := h.getProofreadConfig()
	if err != nil {
		c.JSON(400, gin.H{"status": "error", "error": err.Error()})
		return
	}

	resp, err := h.queryInternal(baseURL, apiKey, taskID)
	if err != nil {
		slog.Error("proofread_query_failed", "user", userName(c), "task_id", taskID, "error", err)
		c.JSON(502, gin.H{"status": "error", "error": "审稿服务请求失败: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		c.JSON(502, gin.H{"status": "error", "error": "读取审稿服务响应失败"})
		return
	}

	// 更新持久化状态
	h.persistFromQuery(taskID, respBody)

	slog.Info("proofread_query", "user", userName(c), "task_id", taskID, "status", resp.StatusCode)

	c.Data(resp.StatusCode, "application/json", respBody)
}

// State 返回当前审稿任务状态（页面刷新后恢复用）
// GET /api/inner/proofread/state
func (h *ProofreadHandler) State(c *gin.Context) {
	taskID, _ := h.store.GetConfig("proofread_current_task_id")
	status, _ := h.store.GetConfig("proofread_current_status")

	c.JSON(200, gin.H{
		"status": "ok",
		"data": gin.H{
			"task_id": taskID,
			"status":  status,
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

// persistFromSubmit 从 rdCLI 提交响应中提取 task_id 并持久化
func (h *ProofreadHandler) persistFromSubmit(respBody []byte) {
	var parsed struct {
		Success bool `json:"success"`
		Data    struct {
			TaskID string `json:"task_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return
	}
	if parsed.Success && parsed.Data.TaskID != "" {
		h.store.SetConfig("proofread_current_task_id", parsed.Data.TaskID, "审稿模拟当前任务 ID")
		h.store.SetConfig("proofread_current_status", "pending", "审稿模拟当前任务状态")
	}
}

// persistFromQuery 从 rdCLI 查询响应中提取状态并持久化
func (h *ProofreadHandler) persistFromQuery(taskID string, respBody []byte) {
	var parsed struct {
		Success bool `json:"success"`
		Data    struct {
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return
	}
	if !parsed.Success {
		return
	}
	status := parsed.Data.Status
	if status == "" {
		return
	}
	// 持久化 task_id（以防万一）和状态
	h.store.SetConfig("proofread_current_task_id", taskID, "审稿模拟当前任务 ID")
	h.store.SetConfig("proofread_current_status", status, "审稿模拟当前任务状态")
}