// Package pipeline 通用任务处理流程编排
//
// 所有对外接口（IF-02/03/04/06）均走统一的 Pipeline 骨架：
//
//	download → extract → translate → render → llm → (post_process) → callback
//
// pipeline.go 只定义通用骨架，不感知具体接口。
// 具体接口的差异化逻辑通过 InterfaceDefinition 的钩子注入。
package pipeline

import (
	"fmt"
	"time"

	"doc_flow_agent/internal/model"
	"doc_flow_agent/internal/store"
)

// StepFunc 步骤函数签名（绑定到 TaskContext 的方法）
type StepFunc func() error

// TaskContext 贯穿整个任务生命周期的上下文
type TaskContext struct {
	// ---- 任务标识 ----
	TaskID string
	LinkID string

	// ---- 任务状态（由 handler/worker 管理） ----
	Status       string // pending / processing / success / failed
	ErrorMessage string

	// ---- 输入 ----
	Interface   *InterfaceDefinition    // 接口定义（代码中硬编码）
	Template    *model.TemplateConfig   // 提示词模板（从 DB 按 ID 读取）
	RawBody     map[string]interface{}  // 解析后的请求 JSON
	RawBodyStr  string                  // 原始请求 JSON 字符串
	CallbackURL string                  // 回调地址

	// ---- 各步骤产出 ----
	DownloadResult *model.DownloadResult       // download 步骤产出
	TranslatedJSON string                      // translate 步骤产出
	RenderedPrompt string                      // render 步骤产出（不含 system prompt）
	SystemPrompt   string                      // render 步骤生成的 system prompt
	LLMRawOutput   string                      // llm 步骤产出
	OutputResult   *model.APIResponse          // wrap 后的最终输出

	// ---- LLM 配置（运行时解析） ----
	LLMCfg      *model.LLMConfig
	Temperature *float64
	MaxTokens   *int

	// ---- 基础能力 ----
	Store store.MetaStore
	Cfg   *model.Config

	// ---- 追踪 ----
	Trace    *model.TaskTrace
	Timeline *TimelineRecorder
}

// Run 执行通用 Pipeline，子步骤按序执行，任一步失败即终止并返回 error
func Run(ctx *TaskContext) error {
	steps := []struct {
		name string
		fn   StepFunc
	}{
		{"pre_process", ctx.runPreProcess},
		{"translate", ctx.runTranslate},
		{"download", ctx.runDownload},
		{"extract", ctx.runExtract},
		{"render", ctx.runRender},
		{"llm", ctx.runLLM},
		{"post_process", ctx.runPostProcess},
	}

	for _, step := range steps {
		start := time.Now()
		if err := step.fn(); err != nil {
			ctx.Timeline.Record(step.name + "_error")
			return fmt.Errorf("%s: %w", step.name, err)
		}
		ctx.Timeline.RecordDuration(step.name, time.Since(start).Milliseconds(), 0)
	}

	return nil
}

// runPreProcess 执行接口自定义的前置处理钩子
func (ctx *TaskContext) runPreProcess() error {
	if ctx.Interface.PreProcess == nil {
		return nil
	}
	return ctx.Interface.PreProcess(ctx)
}

// runPostProcess 执行接口自定义的后置处理钩子
func (ctx *TaskContext) runPostProcess() error {
	if ctx.Interface.PostProcess == nil {
		return nil
	}
	return ctx.Interface.PostProcess(ctx)
}