package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"doc_flow_agent/internal/model"
	"doc_flow_agent/internal/preprocess"
	"doc_flow_agent/internal/store"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// CallHandler 通用调用处理器
type CallHandler struct {
	store  store.MetaStore
	cfg    *model.Config
	worker *AsyncWorker
}

// NewCallHandler 创建处理器
func NewCallHandler(st store.MetaStore, cfg *model.Config) *CallHandler {
	return &CallHandler{store: st, cfg: cfg}
}

// SetWorker 注入 AsyncWorker（由 main.go 在初始化时调用）
func (h *CallHandler) SetWorker(w *AsyncWorker) {
	h.worker = w
}

// Call 通用调用（异步入口）
// POST /api/v1/*interfacePath
// 请求体：完整 JSON，必须含 linkid 和 callback_url
func (h *CallHandler) Call(c *gin.Context) {
	// 1. 获取接口路径（从 URL 通配部分提取）
	interfacePath := strings.TrimPrefix(c.Param("interfacePath"), "/")
	if interfacePath == "" || interfacePath == "/" {
		c.JSON(400, model.NewErrorResponse(model.ErrCodeParamError, "缺少接口路径，正确格式: POST /api/v1/document/classify"))
		return
	}

	// 2. 读取完整请求体
	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(400, model.NewErrorResponse(model.ErrCodeParamError, "读取请求体失败"))
		return
	}
	if len(bodyBytes) == 0 {
		c.JSON(400, model.NewErrorResponse(model.ErrCodeParamError, "请求体为空"))
		return
	}

	var rawBody map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &rawBody); err != nil {
		c.JSON(400, model.NewErrorResponse(model.ErrCodeParamError, "请求体不是合法 JSON: "+err.Error()))
		return
	}

	slog.Info("call_request", "interface", interfacePath, "body", ellipsisJSON(string(bodyBytes), 2000))

	// 3. 查找接口配置
	iface, err := h.store.GetInterfaceByPath(interfacePath)
	if err != nil || iface == nil {
		c.JSON(404, model.NewErrorResponse(model.ErrCodeParamError, "接口不存在: "+interfacePath))
		return
	}
	if !iface.Enabled {
		c.JSON(403, model.NewErrorResponse(model.ErrCodeParamError, "接口已禁用: "+interfacePath))
		return
	}

	// 4. 提取 linkid（必填）
	linkid, _ := rawBody["linkid"].(string)
	if linkid == "" {
		c.JSON(400, ginResponse{Code: model.ErrCodeParamError, Message: "缺少 linkid", TaskID: ""})
		return
	}

	// 5. 提取 callback_url（必填）
	callbackURL, _ := rawBody["callback_url"].(string)
	if callbackURL == "" {
		c.JSON(400, ginResponse{Code: model.ErrCodeParamError, Message: "缺少 callback_url", TaskID: ""})
		return
	}

	// 6. 检查任务队列是否已满（running >= max_pending）
	maxPending := h.cfg.Sys.MaxPendingTasks
	if maxPending <= 0 {
		maxPending = 10
	}
	running, err := h.store.CountRunningTasks()
	if err != nil {
		slog.Error("call_count_running_failed", "error", err)
		c.JSON(500, model.NewErrorResponse(model.ErrCodeInternal, "内部错误"))
		return
	}
	if running >= maxPending {
		maxConcurrent := h.cfg.Sys.MaxConcurrentTasks
		if maxConcurrent <= 0 {
			maxConcurrent = 5
		}
		msg := fmt.Sprintf("任务队列已满，当前处理中任务数 %d/%d（最大并发 %d，最大排队 %d），请稍后重试", running, maxPending, maxConcurrent, maxPending)
		c.JSON(503, ginResponse{Code: model.ErrCodeQueueFull, Message: msg, TaskID: ""})
		return
	}

	// 7. 自生成全局唯一的 task_id（对方 linkid 可能不唯一，不依赖它做唯一键）
	taskID := uuid.New().String()

	// 8. 创建任务追踪记录，存入 DB
	task := &model.TaskTrace{
		TaskID:        taskID,
		LinkID:        linkid,
		InterfacePath: interfacePath,
		RequestBody:   string(bodyBytes),
		CallbackURL:   callbackURL,
		Status:        model.TaskStatusPending,
	}
	if err := h.store.CreateTaskTrace(task); err != nil {
		slog.Error("call_create_task_failed", "error", err, "task_id", taskID, "linkid", linkid)
		c.JSON(500, ginResponse{Code: model.ErrCodeInternal, Message: "任务创建失败", TaskID: ""})
		return
	}

	// 9. 入队 Worker Pool
	h.worker.Enqueue(taskID)

	slog.Info("call_accepted", "task_id", taskID, "linkid", linkid, "interface", interfacePath, "running", running+1, "max", maxPending)
	c.JSON(200, ginResponse{Code: model.ErrCodeSuccess, Message: "success", TaskID: taskID, LinkID: linkid})
}

// ginResponse 异步入口响应格式（含 task_id 和 linkid）
type ginResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	TaskID  string `json:"task_id"`
	LinkID  string `json:"linkid,omitempty"`
}

// fileItem 从请求 JSON 中提取的文件条目
type fileItem struct {
	URL      string
	FileType string // main_body / attachment / doc_basis
	FileName string
}

// downloadedFile 已下载并解析的单个文件
type downloadedFile struct {
	URL        string
	FileName   string
	FileType   string
	LocalPath  string
	Text       string
	HasSeal    bool
	SealTexts  []string
	Status     string // ok / download_failed / extract_failed
	ErrorMsg   string // 失败原因
	DurationMs int64
}

// downloadedFiles 已下载并解析的文件内容，按类别分组
type downloadedFiles struct {
	MainBody    []string         // 正文文本（可能多个，一般一个）
	Attachments []string         // 附件文本
	DocBasis    []string         // 办文依据文本
	SealTexts   []string         // 所有文件中检测到的印章文字（去重）
	FlatItems   []downloadedFile // 扁平列表，供 files_text / files_summary 落库
}

// downloadAllFileURLs 递归提取 body 中所有 file_url，下载、解析并入库
// taskID 用于日志追踪，可为空
func (h *CallHandler) downloadAllFileURLs(body map[string]interface{}, taskID string) *downloadedFiles {
	items := extractFileURLs(body)
	if len(items) == 0 {
		return nil
	}

	result := &downloadedFiles{}
	dlClient := &http.Client{Timeout: 60 * time.Second}

	tmpDir := filepath.Join(h.basePath(), "tmp")
	os.MkdirAll(tmpDir, 0755)

	sealSeen := make(map[string]bool)

	for i, item := range items {
		slog.Info("call_download_file_url", "task_id", taskID, "url", item.URL, "file_type", item.FileType)

		itemStart := time.Now()
		dl := downloadedFile{URL: item.URL, FileType: item.FileType, FileName: item.FileName}

		// 下载文件
		resp, err := dlClient.Get(item.URL)
		if err != nil {
			slog.Warn("call_download_failed", "task_id", taskID, "url", item.URL, "error", err)
			dl.Status = "download_failed"
			dl.ErrorMsg = err.Error()
			dl.DurationMs = time.Since(itemStart).Milliseconds()
			result.FlatItems = append(result.FlatItems, dl)
			continue
		}

		fileBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil || resp.StatusCode != http.StatusOK {
			slog.Warn("call_download_failed", "task_id", taskID, "url", item.URL, "status", resp.StatusCode, "error", err)
			dl.Status = "download_failed"
			if err != nil {
				dl.ErrorMsg = err.Error()
			} else {
				dl.ErrorMsg = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(fileBody[:min(len(fileBody), 200)]))
			}
			dl.DurationMs = time.Since(itemStart).Milliseconds()
			result.FlatItems = append(result.FlatItems, dl)
			continue
		}

		// 确定文件扩展名（从 URL 或 Content-Type 推断）
		ext := detectExtFromURL(item.URL, resp.Header.Get("Content-Type"))

		// 写入临时文件
		tmpFile := filepath.Join(tmpDir, fmt.Sprintf("dl_%d_%d%s", time.Now().UnixNano(), i, ext))
		if err := os.WriteFile(tmpFile, fileBody, 0644); err != nil {
			slog.Warn("call_tmp_write_failed", "task_id", taskID, "url", item.URL, "error", err)
			dl.Status = "download_failed"
			dl.ErrorMsg = err.Error()
			dl.DurationMs = time.Since(itemStart).Milliseconds()
			result.FlatItems = append(result.FlatItems, dl)
			continue
		}
		defer os.Remove(tmpFile)

		dl.LocalPath = tmpFile

		// 提取文本及印章信息
		pd := h.extractFileText(tmpFile)
		if pd == nil || pd.Text == "" {
			slog.Warn("call_extract_empty", "task_id", taskID, "url", item.URL)
			dl.Status = "extract_failed"
			dl.DurationMs = time.Since(itemStart).Milliseconds()
			result.FlatItems = append(result.FlatItems, dl)
			continue
		}
		dl.Text = pd.Text
		dl.HasSeal = pd.HasSeal
		dl.SealTexts = pd.SealText
		dl.Status = "ok"
		dl.DurationMs = time.Since(itemStart).Milliseconds()

		// 入库：file_url 下载的文件无本地持久路径，用 D- 前缀区分
		fileID := fmt.Sprintf("D-%s%04d", time.Now().Format("20060102150405"), time.Now().Nanosecond()%10000)
		record := &model.FileRecord{
			FileID:        fileID,
			OriginalName:  firstNonEmpty(item.FileName, filepath.Base(item.URL)),
			FileType:      detectFileType(ext),
			FilePath:      "",
			TextContent:   pd.Text,
			TextLength:    len(pd.Text),
			OCRConfidence: pd.OCRConfidence,
			HasSeal:       pd.HasSeal,
			SealText:      pd.SealText,
			Status:        model.FileStatusReady,
		}
		if err := h.store.CreateFileRecord(record); err != nil {
			slog.Warn("call_download_store_failed", "task_id", taskID, "url", item.URL, "error", err)
		}

		// 汇总印章文字
		for _, s := range pd.SealText {
			if s != "" && !sealSeen[s] {
				sealSeen[s] = true
				result.SealTexts = append(result.SealTexts, s)
			}
		}

		// 带文件名和印章信息前缀的文本块
		block := pd.Text
		if item.FileName != "" {
			block = fmt.Sprintf("【%s】\n%s", item.FileName, block)
		}
		if len(pd.SealText) > 0 {
			block = block + "\n（印章文字：" + strings.Join(pd.SealText, "、") + "）"
		}

		switch item.FileType {
		case "main_body":
			result.MainBody = append(result.MainBody, block)
		case "attachment":
			result.Attachments = append(result.Attachments, block)
		case "doc_basis":
			result.DocBasis = append(result.DocBasis, block)
		default:
			// 未指定类型，默认归入正文
			result.MainBody = append(result.MainBody, block)
		}

		slog.Info("call_download_done", "task_id", taskID, "url", item.URL, "file_type", item.FileType, "text_len", len(pd.Text), "has_seal", pd.HasSeal)
		result.FlatItems = append(result.FlatItems, dl)
	}

	return result
}

// extractFileURLs 递归从 JSON 中提取所有 file_url（连同同级的 file_type/file_name）
func extractFileURLs(v interface{}) []fileItem {
	var items []fileItem
	seen := make(map[string]bool)

	var walk func(v interface{})
	walk = func(v interface{}) {
		switch val := v.(type) {
		case map[string]interface{}:
			// 若当前对象本身含 file_url，则作为文件条目处理
			if url, ok := val["file_url"].(string); ok && url != "" && !seen[url] {
				seen[url] = true
				item := fileItem{URL: url}
				if ft, ok := val["file_type"].(string); ok {
					item.FileType = ft
				}
				if fn, ok := val["file_name"].(string); ok {
					item.FileName = fn
				}
				items = append(items, item)
			}
			// 继续递归遍历其他字段（包括 files 数组内对象等）
			for k, vv := range val {
				if k == "file_url" {
					continue
				}
				walk(vv)
			}
		case []interface{}:
			for _, item := range val {
				walk(item)
			}
		}
	}
	walk(v)
	return items
}

// firstNonEmpty 返回第一个非空字符串
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// detectExtFromURL 从 URL 路径或 Content-Type 推断文件扩展名
func detectExtFromURL(url string, contentType string) string {
	// 去掉 query string
	if idx := strings.Index(url, "?"); idx >= 0 {
		url = url[:idx]
	}
	ext := strings.ToLower(filepath.Ext(url))
	if ext == ".pdf" || ext == ".docx" || ext == ".xlsx" || ext == ".ofd" ||
		ext == ".doc" || ext == ".xls" || ext == ".png" || ext == ".jpg" || ext == ".jpeg" {
		return ext
	}

	// 从 Content-Type 推断
	switch {
	case strings.Contains(contentType, "pdf"):
		return ".pdf"
	case strings.Contains(contentType, "word") || strings.Contains(contentType, "docx"):
		return ".docx"
	case strings.Contains(contentType, "excel") || strings.Contains(contentType, "xlsx") || strings.Contains(contentType, "spreadsheet"):
		return ".xlsx"
	case strings.Contains(contentType, "image"):
		return ".png"
	default:
		return ".pdf" // 默认假设 PDF
	}
}

// extractFileText 提取文件文本及印章信息（本地或远程解析服务）
func (h *CallHandler) extractFileText(localPath string) *model.ParsedDocument {
	if h.cfg.DocParser.URL != "" {
		if pd, err := h.extractTextRemote(localPath); err == nil {
			return pd
		}
		slog.Warn("call_remote_parse_failed", "path", localPath)
	}

	// 本地解析
	result, err := preprocess.ExtractText(localPath)
	if err != nil {
		slog.Warn("call_local_parse_failed", "path", localPath, "error", err)
		return nil
	}
	return &model.ParsedDocument{
		Text:          result.Text,
		OCRConfidence: result.OCRConfidence,
		HasSeal:       false,
		SealText:      []string{},
	}
}

// extractTextRemote 调用外部文档解析服务，返回完整解析结果
func (h *CallHandler) extractTextRemote(localPath string) (*model.ParsedDocument, error) {
	f, err := os.Open(localPath)
	if err != nil {
		return nil, fmt.Errorf("打开文件失败: %w", err)
	}
	defer f.Close()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filepath.Base(localPath))
	if err != nil {
		return nil, fmt.Errorf("创建 form 字段失败: %w", err)
	}
	if _, err := io.Copy(part, f); err != nil {
		return nil, fmt.Errorf("写入文件数据失败: %w", err)
	}
	writer.Close()

	req, err := http.NewRequest(http.MethodPost, h.cfg.DocParser.URL, &body)
	if err != nil {
		return nil, fmt.Errorf("创建解析请求失败: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if h.cfg.DocParser.Token != "" {
		req.Header.Set("Authorization", "Bearer "+h.cfg.DocParser.Token)
	}

	resp, err := docParserClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("调用文档解析服务失败: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取解析响应失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("文档解析服务返回错误状态码 %d: %s", resp.StatusCode, string(respBody))
	}

	pd := &model.ParsedDocument{}
	if err := json.Unmarshal(respBody, pd); err != nil {
		return nil, fmt.Errorf("解析 JSON 响应失败: %w", err)
	}
	return pd, nil
}

// translateJSON 将 JSON 中出现的 key 按 dict 翻译为中文，递归处理嵌套结构
func translateJSON(v interface{}, dict map[string]string) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(val))
		for k, vv := range val {
			newKey := k
			if cn, ok := dict[k]; ok && cn != "" {
				newKey = cn
			}
			out[newKey] = translateJSON(vv, dict)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(val))
		for i, item := range val {
			out[i] = translateJSON(item, dict)
		}
		return out
	default:
		return v
	}
}

// renderPrompt 渲染提示词模板
func (h *CallHandler) renderPrompt(iface *model.InterfaceConfig, requestBody string, files *downloadedFiles) (string, error) {
	result := iface.PromptTemplate

	// 替换 {{request.body}} — 完整请求 JSON
	result = replaceAll(result, "{{request.body}}", requestBody)
	result = replaceAll(result, "{{request.json}}", requestBody) // 别名

	// 数据字典翻译后的 JSON
	if dict := h.cfg.Dict; len(dict) > 0 {
		var raw map[string]interface{}
		if json.Unmarshal([]byte(requestBody), &raw) == nil {
			translated := translateJSON(raw, dict)
			translatedJSON := mustJSON(translated)
			result = replaceAll(result, "{{request.dict}}", translatedJSON)
			result = replaceAll(result, "{{request.translated}}", translatedJSON) // 别名
		}
	}

	if files != nil {
		// {{file.main_body}} — 正文内容
		if len(files.MainBody) > 0 {
			result = replaceAll(result, "{{file.main_body}}", strings.Join(files.MainBody, "\n\n"))
		} else {
			result = replaceAll(result, "{{file.main_body}}", "")
		}
		// {{file.attachments}} — 附件内容
		if len(files.Attachments) > 0 {
			result = replaceAll(result, "{{file.attachments}}", strings.Join(files.Attachments, "\n\n"))
		} else {
			result = replaceAll(result, "{{file.attachments}}", "")
		}
		// {{file.doc_basis}} — 办文依据内容
		if len(files.DocBasis) > 0 {
			result = replaceAll(result, "{{file.doc_basis}}", strings.Join(files.DocBasis, "\n\n"))
		} else {
			result = replaceAll(result, "{{file.doc_basis}}", "")
		}
		// {{file.seal_text}} — 所有印章文字汇总
		if len(files.SealTexts) > 0 {
			result = replaceAll(result, "{{file.seal_text}}", strings.Join(files.SealTexts, "、"))
		} else {
			result = replaceAll(result, "{{file.seal_text}}", "无")
		}
		// {{file.all_text}} — 所有文件文本聚合
		var allParts []string
		for _, t := range files.MainBody {
			allParts = append(allParts, "=== 正文 ===\n"+t)
		}
		for _, t := range files.Attachments {
			allParts = append(allParts, "=== 附件 ===\n"+t)
		}
		for _, t := range files.DocBasis {
			allParts = append(allParts, "=== 办文依据 ===\n"+t)
		}
		result = replaceAll(result, "{{file.all_text}}", strings.Join(allParts, "\n\n"))
	}

	// 兼容旧的占位符（置空）
	result = replaceAll(result, "{{file.text}}", "")
	result = replaceAll(result, "{{file.name}}", "")
	result = replaceAll(result, "{{file.type}}", "")

	return result, nil
}

// getLLMConfig 获取 LLM 配置
func (h *CallHandler) getLLMConfig(iface *model.InterfaceConfig) (*model.LLMConfig, error) {
	if iface.LLMConfigID != nil && *iface.LLMConfigID > 0 {
		cfg, err := h.store.GetLLMConfig(*iface.LLMConfigID)
		if err == nil && cfg != nil {
			return cfg, nil
		}
		slog.Warn("call_llm_config_not_found", "id", *iface.LLMConfigID)
	}

	// 使用默认 LLM
	cfg, err := h.store.GetDefaultLLMConfig()
	if err != nil || cfg == nil {
		return nil, fmt.Errorf("未配置 LLM 模型，请先在管理后台配置默认模型")
	}
	return cfg, nil
}

// buildSystemPrompt 构造 system prompt，将输出 schema 注入其中，引导 LLM 按指定 JSON 结构返回
func (h *CallHandler) buildSystemPrompt(iface *model.InterfaceConfig) string {
	prompt := "你是一个电子公文处理助手。请严格按照要求的 JSON 格式返回结果，不要包含任何其他文字。"

	if schema := strings.TrimSpace(iface.OutputSchema); schema != "" && schema != "{}" {
		prompt += "\n\n请严格按以下 JSON Schema 返回结果：\n```json\n" + schema + "\n```"
	}

	return prompt
}

// wrapOutput 将 LLM 原始输出包装为统一响应格式
func (h *CallHandler) wrapOutput(raw string, schema string) model.APIResponse {
	raw = strings.TrimSpace(raw)

	// 尝试将 raw 解析为 JSON
	var parsed interface{}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		// 不是合法 JSON，直接从第一个 { 或 [ 提取
		start := strings.Index(raw, "{")
		if start < 0 {
			start = strings.Index(raw, "[")
		}
		if start >= 0 {
			_ = json.Unmarshal([]byte(raw[start:]), &parsed)
		}
		if parsed == nil {
			// 无法解析，当作纯文本包在 data 里
			return model.NewSuccessResponse(raw)
		}
	}

	// 如果 LLM 已经按 {code, message, data} 格式输出，原样返回
	if m, ok := parsed.(map[string]interface{}); ok {
		if _, hasCode := m["code"]; hasCode {
			return model.APIResponse{
				Code:    model.ErrCodeSuccess,
				Message: "success",
				Data:    m,
			}
		}
		// 如果 LLM 输出的是 data 内容（如 {linkid, need_human, ...}），包一层
		return model.NewSuccessResponse(m)
	}

	return model.NewSuccessResponse(parsed)
}

// mustJSON 序列化 JSON，失败返回 {}
func mustJSON(v interface{}) string {
	if v == nil {
		return "{}"
	}
	data, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(data)
}

// ellipsisJSON 截断 JSON 文本
func ellipsisJSON(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func (h *CallHandler) basePath() string {
	if h.cfg.Sys.BasePath != "" {
		return h.cfg.Sys.BasePath
	}
	return "data"
}
