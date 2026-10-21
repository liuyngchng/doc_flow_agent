package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"doc-ai-gateway/internal/llm"
	"doc-ai-gateway/internal/model"
	"doc-ai-gateway/internal/store"

	"github.com/gin-gonic/gin"
)

// CallHandler 通用调用处理器
type CallHandler struct {
	store store.MetaStore
}

// NewCallHandler 创建处理器
func NewCallHandler(st store.MetaStore) *CallHandler {
	return &CallHandler{store: st}
}

// Call 通用调用
// POST /api/call
// 支持 Content-Type: application/json、text/plain、application/x-www-form-urlencoded
func (h *CallHandler) Call(c *gin.Context) {
	var fileID, ifacePath string
	var params map[string]interface{}
	var virtualFile *model.FileRecord

	contentType := c.ContentType()

	if strings.HasPrefix(contentType, "text/plain") {
		// text/plain: body 为纯文本，interface 通过 query 参数指定
		bodyBytes, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.JSON(400, gin.H{"error": "读取请求体失败"})
			return
		}
		plainText := strings.TrimSpace(string(bodyBytes))
		if plainText == "" {
			c.JSON(400, gin.H{"error": "请求体为空"})
			return
		}
		ifacePath = c.Query("interface")
		if ifacePath == "" {
			c.JSON(400, gin.H{"error": "缺少 interface 参数"})
			return
		}
		fileID = "TEXT-" + fmt.Sprintf("%05d", time.Now().UnixNano()%100000)
		params = make(map[string]interface{})
		// 构造虚拟文件记录
		virtualFile = &model.FileRecord{
			FileID:       fileID,
			OriginalName: "plain_text.txt",
			FileType:     "text",
			TextContent:  plainText,
			TextLength:   len(plainText),
			Status:       model.FileStatusReady,
		}
	} else if strings.HasPrefix(contentType, "application/x-www-form-urlencoded") {
		// form 表单：file_id、interface 固定字段，其余字段全部收进 params 给 LLM
		if err := c.Request.ParseForm(); err != nil {
			c.JSON(400, gin.H{"error": "解析表单失败"})
			return
		}
		ifacePath = c.PostForm("interface")
		fileID = c.PostForm("file_id")
		if fileID == "" || ifacePath == "" {
			c.JSON(400, gin.H{"error": "file_id 和 interface 不能为空"})
			return
		}
		params = make(map[string]interface{})
		for k, vs := range c.Request.PostForm {
			if k == "file_id" || k == "interface" {
				continue
			}
			if len(vs) == 1 {
				params[k] = vs[0]
			} else {
				params[k] = vs
			}
		}
	} else {
		// application/json (默认)
		var req struct {
			FileID    string                 `json:"file_id"`
			Interface string                 `json:"interface"`
			Params    map[string]interface{} `json:"params"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, gin.H{"error": "参数错误"})
			return
		}
		if req.FileID == "" || req.Interface == "" {
			c.JSON(400, gin.H{"error": "file_id 和 interface 不能为空"})
			return
		}
		fileID = req.FileID
		ifacePath = req.Interface
		params = req.Params
	}

	slog.Info("call_request", "interface", ifacePath, "file_id", fileID, "params", mustJSON(params))

	startTime := time.Now()

	// 1. 查找接口配置
	iface, err := h.store.GetInterfaceByPath(ifacePath)
	if err != nil || iface == nil {
		c.JSON(404, gin.H{"error": "接口不存在: " + ifacePath})
		return
	}
	if !iface.Enabled {
		c.JSON(403, gin.H{"error": "接口已禁用: " + ifacePath})
		return
	}

	// 2. 获取文件内容
	var file *model.FileRecord
	if virtualFile != nil {
		file = virtualFile
	} else {
		file, err = h.store.GetFileRecord(fileID)
		if err != nil || file == nil {
			c.JSON(404, gin.H{"error": "文件不存在: " + fileID})
			return
		}
		if file.Status == model.FileStatusProcessing {
			c.JSON(202, gin.H{"error": "文件预处理中，请稍后重试", "file_id": fileID})
			return
		}
		if file.Status == model.FileStatusFailed {
			c.JSON(422, gin.H{"error": "文件预处理失败: " + file.ErrorMessage, "file_id": fileID})
			return
		}
	}

	// 3. 渲染 prompt
	systemPrompt := "你是一个公文处理助手。请严格按照要求的 JSON 格式返回结果，不要包含任何其他文字。"
	userPrompt, err := h.renderPrompt(iface, file, params)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// 4. 获取 LLM 配置
	llmCfg, err := h.getLLMConfig(iface)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	// 5. 调用 LLM
	client := llm.New(llmCfg.APIURI, llmCfg.APIKey, llmCfg.ModelName)

	temperature := iface.Temperature
	if temperature == nil {
		t := llmCfg.Temperature
		temperature = &t
	}
	maxTokens := iface.MaxTokens
	if maxTokens == nil {
		mt := llmCfg.MaxTokens
		maxTokens = &mt
	}

	resultText, err := client.Chat(systemPrompt, userPrompt, temperature, maxTokens)
	elapsedMs := time.Since(startTime).Milliseconds()

	// 6. 记录日志
	logEntry := &model.CallLog{
		FileID:        fileID,
		InterfaceID:   iface.ID,
		InterfacePath: ifacePath,
		InputParams:   mustJSON(params),
		ElapsedMs:     elapsedMs,
	}

	if err != nil {
		logEntry.Status = "error"
		logEntry.ErrorMessage = err.Error()
		logEntry.OutputResult = "{}"
		h.store.CreateCallLog(logEntry)

		slog.Error("call_llm_error", "interface", ifacePath, "file_id", fileID, "error", err)
		slog.Info("call_response", "interface", ifacePath, "file_id", fileID, "status", "error", "error_msg", err.Error(), "elapsed_ms", elapsedMs)
		c.JSON(500, gin.H{"error": "调用失败: " + err.Error()})
		return
	}

	// 7. 解析 + 校验输出
	output, parseErr := h.parseOutput(resultText, iface.OutputSchema)
	logEntry.OutputResult = mustJSON(output)
	logEntry.Status = "success"
	if parseErr != nil {
		logEntry.ErrorMessage = parseErr.Error()
		logEntry.Status = "error"
		slog.Warn("call_output_parse_warning", "interface", ifacePath, "error", parseErr)
		h.store.CreateCallLog(logEntry)
		slog.Info("call_response", "interface", ifacePath, "file_id", fileID, "status", "warning", "result", mustJSON(output), "elapsed_ms", elapsedMs)
		c.JSON(200, gin.H{
			"file_id":    fileID,
			"interface":  ifacePath,
			"status":     "warning",
			"result":     output,
			"raw_output": resultText,
			"warning":    parseErr.Error(),
			"elapsed_ms": elapsedMs,
		})
		return
	}

	h.store.CreateCallLog(logEntry)

	slog.Info("call_response", "interface", ifacePath, "file_id", fileID, "status", "success", "result", mustJSON(output), "elapsed_ms", elapsedMs)

	c.JSON(200, gin.H{
		"file_id":    fileID,
		"interface":  ifacePath,
		"status":     "success",
		"result":     output,
		"elapsed_ms": elapsedMs,
	})
}

// renderPrompt 渲染提示词模板
func (h *CallHandler) renderPrompt(iface *model.InterfaceConfig, file *model.FileRecord, params map[string]interface{}) (string, error) {
	result := iface.PromptTemplate

	// 替换文件占位符
	result = replaceAll(result, "{{file.text}}", file.TextContent)
	result = replaceAll(result, "{{file.name}}", file.OriginalName)
	result = replaceAll(result, "{{file.type}}", file.FileType)

	// 替换自定义参数
	for _, ip := range iface.InputParams {
		placeholder := "{{" + ip.Source + "}}"
		if ip.Source == "file.text" || ip.Source == "file.name" || ip.Source == "file.type" {
			continue // 已在上面处理
		}

		// params.xxx
		if strings.HasPrefix(ip.Source, "params.") {
			key := strings.TrimPrefix(ip.Source, "params.")
			if val, ok := params[key]; ok {
				result = replaceAll(result, placeholder, fmt.Sprintf("%v", val))
			} else if ip.Required {
				return "", fmt.Errorf("缺少必填参数: %s (%s)", ip.Name, ip.Source)
			} else {
				result = replaceAll(result, placeholder, ip.Default)
			}
		}
	}

	// 兼容处理：直接 {{params.xxx}} 格式（无需在 input_params 中定义）
	for k, v := range params {
		placeholder := "{{params." + k + "}}"
		valStr := fmt.Sprintf("%v", v)
		if strings.Contains(result, placeholder) {
			result = replaceAll(result, placeholder, valStr)
		}
	}

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

// parseOutput 解析输出
func (h *CallHandler) parseOutput(raw string, schema string) (interface{}, error) {
	// 尝试提取 JSON（LLM 有时会在前后加说明）
	raw = strings.TrimSpace(raw)

	// 找第一个 { 或 [
	start := strings.Index(raw, "{")
	if start < 0 {
		start = strings.Index(raw, "[")
	}
	if start < 0 {
		return raw, fmt.Errorf("输出不是 JSON 格式")
	}

	endJSON := raw[start:]
	// 找匹配的闭合
	braceCount := 0
	end := -1
	for i, ch := range endJSON {
		switch ch {
		case '{':
			braceCount++
		case '}':
			braceCount--
			if braceCount == 0 {
				end = i + 1
				goto done
			}
		}
	}
done:
	if end < 0 {
		return raw[start:], fmt.Errorf("JSON 格式不完整")
	}

	jsonStr := endJSON[:end]
	var result interface{}
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return jsonStr, fmt.Errorf("JSON 解析失败: %w", err)
	}

	return result, nil
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