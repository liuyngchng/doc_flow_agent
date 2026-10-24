package pipeline

import (
	"fmt"
	"strings"
)

// ============================================================
// render 步骤：渲染提示词模板（通过接口路径获取模板内容）
// ============================================================

func (ctx *TaskContext) runRender() error {
	// 模板已在 Run 前通过 LoadTemplate 加载
	if ctx.Template == nil {
		return fmt.Errorf("未加载提示词模板")
	}

	// 1. 渲染 user prompt
	userPrompt, err := ctx.renderPrompt()
	if err != nil {
		return err
	}
	ctx.RenderedPrompt = userPrompt

	// 2. 构造 system prompt（注入输出 schema）
	ctx.SystemPrompt = ctx.buildSystemPrompt()

	// 3. 解析 LLM 配置
	if err := ctx.resolveLLMConfig(); err != nil {
		return err
	}

	return nil
}

// renderPrompt 替换模板中的占位符
func (ctx *TaskContext) renderPrompt() (string, error) {
	result := ctx.Template.PromptTemplate

	// {{request.body}} / {{request.json}} — 完整请求 JSON
	result = replaceAll(result, "{{request.body}}", ctx.RawBodyStr)
	result = replaceAll(result, "{{request.json}}", ctx.RawBodyStr)

	// {{request.dict}} / {{request.translated}} — 翻译后的 JSON
	if ctx.TranslatedJSON != "" {
		result = replaceAll(result, "{{request.dict}}", ctx.TranslatedJSON)
		result = replaceAll(result, "{{request.translated}}", ctx.TranslatedJSON)
	}

	files := ctx.DownloadResult
	if files != nil {
		// {{file.main_body}}
		if len(files.MainBody) > 0 {
			result = replaceAll(result, "{{file.main_body}}", strings.Join(files.MainBody, "\n\n"))
		} else {
			result = replaceAll(result, "{{file.main_body}}", "")
		}
		// {{file.attachments}}
		if len(files.Attachments) > 0 {
			result = replaceAll(result, "{{file.attachments}}", strings.Join(files.Attachments, "\n\n"))
		} else {
			result = replaceAll(result, "{{file.attachments}}", "")
		}
		// {{file.doc_basis}}
		if len(files.DocBasis) > 0 {
			result = replaceAll(result, "{{file.doc_basis}}", strings.Join(files.DocBasis, "\n\n"))
		} else {
			result = replaceAll(result, "{{file.doc_basis}}", "")
		}
		// {{file.seal_text}}
		if len(files.SealTexts) > 0 {
			result = replaceAll(result, "{{file.seal_text}}", strings.Join(files.SealTexts, "、"))
		} else {
			result = replaceAll(result, "{{file.seal_text}}", "无")
		}
		// {{file.all_text}}
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

	// 兼容旧占位符
	result = replaceAll(result, "{{file.text}}", "")
	result = replaceAll(result, "{{file.name}}", "")
	result = replaceAll(result, "{{file.type}}", "")

	return result, nil
}

// buildSystemPrompt 构造 system prompt，注入输出 schema
func (ctx *TaskContext) buildSystemPrompt() string {
	prompt := "你是一个电子公文处理助手。请严格按照要求的 JSON 格式返回结果，不要包含任何其他文字。"

	if schema := strings.TrimSpace(ctx.Template.OutputSchema); schema != "" && schema != "{}" {
		prompt += "\n\n请严格按以下 JSON Schema 返回结果：\n```json\n" + schema + "\n```"
	}

	return prompt
}

// resolveLLMConfig 解析 LLM 配置（模板指定 > 默认）
func (ctx *TaskContext) resolveLLMConfig() error {
	if ctx.Template.LLMConfigID != nil && *ctx.Template.LLMConfigID > 0 {
		cfg, err := ctx.Store.GetLLMConfig(*ctx.Template.LLMConfigID)
		if err == nil && cfg != nil {
			ctx.LLMCfg = cfg
		}
	}

	if ctx.LLMCfg == nil {
		cfg, err := ctx.Store.GetDefaultLLMConfig()
		if err != nil || cfg == nil {
			return fmt.Errorf("未配置 LLM 模型，请先在管理后台配置默认模型")
		}
		ctx.LLMCfg = cfg
	}

	// temperature
	if ctx.Template.Temperature != nil {
		ctx.Temperature = ctx.Template.Temperature
	} else {
		t := ctx.LLMCfg.Temperature
		ctx.Temperature = &t
	}

	// max_tokens
	if ctx.Template.MaxTokens != nil {
		ctx.MaxTokens = ctx.Template.MaxTokens
	} else {
		mt := ctx.LLMCfg.MaxTokens
		ctx.MaxTokens = &mt
	}

	return nil
}

// LoadTemplate 按接口路径从 DB 加载模板到上下文
func (ctx *TaskContext) LoadTemplate() error {
	tmpl, err := ctx.Store.GetTemplateByPath(ctx.Interface.Path)
	if err != nil {
		return fmt.Errorf("加载模板失败: %w", err)
	}
	if tmpl == nil || !tmpl.Enabled {
		return fmt.Errorf("接口 %s 未配置模板，请在管理后台创建模板并关联此接口路径", ctx.Interface.Path)
	}
	ctx.Template = tmpl
	return nil
}

// replaceAll 字符串替换（封装 strings.ReplaceAll）
func replaceAll(s, old, new string) string {
	return strings.ReplaceAll(s, old, new)
}
