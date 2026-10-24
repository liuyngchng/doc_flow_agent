package pipeline

import (
	"encoding/json"
	"time"

	"doc_flow_agent/internal/model"
)

// InterfaceDefinition 代码中定义的接口
type InterfaceDefinition struct {
	Path        string // "document/classify"
	Name        string // "收文分类"
	Description string

	// 可选钩子
	PreProcess  func(ctx *TaskContext) error // 在 download 之前
	PostProcess func(ctx *TaskContext) error // 在 LLM 之后、callback 之前
}

// TimelineRecorder 时间线记录器
type TimelineRecorder struct {
	Entries []model.TimelineEntry
}

// NewTimeline 创建时间线记录器
func NewTimeline() *TimelineRecorder {
	return &TimelineRecorder{}
}

// Record 记录一个步骤
func (tr *TimelineRecorder) Record(step string) {
	tr.Entries = append(tr.Entries, model.TimelineEntry{
		Step: step,
		At:   time.Now().Format("2006-01-02T15:04:05.000-07:00"),
	})
}

// RecordDuration 记录带耗时的步骤
func (tr *TimelineRecorder) RecordDuration(step string, durMs int64, attempt int) {
	e := model.TimelineEntry{
		Step:       step,
		At:         time.Now().Format("2006-01-02T15:04:05.000-07:00"),
		DurationMs: durMs,
	}
	if attempt > 0 {
		e.Attempt = attempt
	}
	tr.Entries = append(tr.Entries, e)
}

// Marshal 序列化为 JSON 字符串
func (tr *TimelineRecorder) Marshal() string {
	b, _ := json.Marshal(tr.Entries)
	return string(b)
}