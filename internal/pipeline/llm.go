package pipeline

import (
	"fmt"

	"doc_flow_agent/internal/llm"
)

// ============================================================
// llm 步骤：调用大模型
// ============================================================

func (ctx *TaskContext) runLLM() error {
	client := llm.New(ctx.LLMCfg.APIURI, ctx.LLMCfg.APIKey, ctx.LLMCfg.ModelName)

	resultText, err := client.Chat(ctx.SystemPrompt, ctx.RenderedPrompt, ctx.Temperature, ctx.MaxTokens)
	if err != nil {
		return fmt.Errorf("LLM 调用失败: %w", err)
	}

	ctx.LLMRawOutput = resultText
	return nil
}