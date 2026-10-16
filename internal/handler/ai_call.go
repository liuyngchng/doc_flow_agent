package handler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"doc-ai-gateway/internal/llm"
	"doc-ai-gateway/internal/model"
	"doc-ai-gateway/internal/store"

	"github.com/gin-gonic/gin"
)

// AICallHandler 通用 AI 调用处理器
type AICallHandler struct {
	store store.MetaStore
}

// NewAICallHandler 创建处理器
func NewAICallHandler(st store.MetaStore) *AICallHandler {
	return &AICallHandler{store: st}
}

// Call 通用 AI 调用 (电子公文工作流调用)
// POST /api/ai/call
func (h *AICallHandler) Call(c *gin.Context) {
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

	startTime := time.Now()

	// 1. 查找接口配置
	iface, err := h.store.GetAIInterfaceByPath(req.Interface)
	if err != nil || iface == nil {
		c.JSON(404, gin.H{"error": "AI 接口不存在: " + req.Interface})
		return
	}
	if !iface.Enabled {
		c.JSON(403, gin.H{"error": "AI 接口已禁用: " + req.Interface})
		return
	}

	// 2. 查找文件记录
	file, err := h.store.GetFileRecord(req.FileID)
	if err != nil || file == nil {
		c.JSON(404, gin.H{"error": "文件不存在: " + req.FileID})
		return
	}
	if file.Status == model.FileStatusProcessing {
		c.JSON(202, gin.H{"error": "文件预处理中，请稍后重试", "file_id": req.FileID})
		return
	}
	if file.Status == model.FileStatusFailed {
		c.JSON(422, gin.H{"error": "文件预处理失败: " + file.ErrorMessage, "file_id": req.FileID})
		return
	}

	// 3. 渲染 prompt
	systemPrompt := "你是一个公文处理助手。请严格按照要求的 JSON 格式返回结果，不要包含任何其他文字。"
	userPrompt, err := h.renderPrompt(iface, file, req.Params)
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
		FileID:        req.FileID,
		InterfaceID:   iface.ID,
		InterfacePath: req.Interface,
		InputParams:   mustJSON(req.Params),
		ElapsedMs:     elapsedMs,
	}

	if err != nil {
		logEntry.Status = "error"
		logEntry.ErrorMessage = err.Error()
		logEntry.OutputResult = "{}"
		h.store.CreateCallLog(logEntry)

		slog.Error("ai_call_llm_error", "interface", req.Interface, "file_id", req.FileID, "error", err)
		c.JSON(500, gin.H{"error": "AI 调用失败: " + err.Error()})
		return
	}

	// 7. 解析 + 校验输出
	output, parseErr := h.parseAIOutput(resultText, iface.OutputSchema)
	logEntry.OutputResult = mustJSON(output)
	logEntry.Status = "success"
	if parseErr != nil {
		logEntry.ErrorMessage = parseErr.Error()
		logEntry.Status = "error"
		slog.Warn("ai_call_output_parse_warning", "interface", req.Interface, "error", parseErr)
		h.store.CreateCallLog(logEntry)
		c.JSON(200, gin.H{
			"file_id":    req.FileID,
			"interface":  req.Interface,
			"status":     "warning",
			"result":     output,
			"raw_output": resultText,
			"warning":    parseErr.Error(),
			"elapsed_ms": elapsedMs,
		})
		return
	}

	h.store.CreateCallLog(logEntry)

	c.JSON(200, gin.H{
		"file_id":    req.FileID,
		"interface":  req.Interface,
		"status":     "success",
		"result":     output,
		"elapsed_ms": elapsedMs,
	})
}

// renderPrompt 渲染提示词模板
func (h *AICallHandler) renderPrompt(iface *model.AIInterface, file *model.FileRecord, params map[string]interface{}) (string, error) {
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
func (h *AICallHandler) getLLMConfig(iface *model.AIInterface) (*model.LLMConfig, error) {
	if iface.LLMConfigID != nil && *iface.LLMConfigID > 0 {
		cfg, err := h.store.GetLLMConfig(*iface.LLMConfigID)
		if err == nil && cfg != nil {
			return cfg, nil
		}
		slog.Warn("ai_call_llm_config_not_found", "id", *iface.LLMConfigID)
	}

	// 使用默认 LLM
	cfg, err := h.store.GetDefaultLLMConfig()
	if err != nil || cfg == nil {
		return nil, fmt.Errorf("未配置 LLM 模型，请先在管理后台配置默认模型")
	}
	return cfg, nil
}

// parseAIOutput 解析 AI 输出
func (h *AICallHandler) parseAIOutput(raw string, schema string) (interface{}, error) {
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