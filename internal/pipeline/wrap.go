package pipeline

import (
	"encoding/json"
	"strings"

	"doc_flow_agent/internal/model"
)

// ============================================================
// wrap 步骤：将 LLM 原始输出包装为统一响应格式
// ============================================================

// WrapOutput 将 LLM 原始输出包装为统一响应格式
func WrapOutput(raw string, schema string) model.APIResponse {
	raw = strings.TrimSpace(raw)

	var parsed interface{}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		start := strings.Index(raw, "{")
		if start < 0 {
			start = strings.Index(raw, "[")
		}
		if start >= 0 {
			_ = json.Unmarshal([]byte(raw[start:]), &parsed)
		}
		if parsed == nil {
			return model.NewSuccessResponse(raw)
		}
	}

	if m, ok := parsed.(map[string]interface{}); ok {
		if _, hasCode := m["code"]; hasCode {
			return model.APIResponse{
				Code:    model.ErrCodeSuccess,
				Message: "success",
				Data:    m,
			}
		}
		return model.NewSuccessResponse(m)
	}

	return model.NewSuccessResponse(parsed)
}