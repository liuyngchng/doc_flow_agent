package pipeline

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// docParserClient 文档解析服务专用 HTTP 客户端（跳过 TLS 证书验证，兼容自签证书）
var docParserClient = &http.Client{
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	},
}

// ============================================================
// external 步骤：调用其他接口（通用 HTTP 调用）
// 用于需要额外调用外部服务的场景（如 #4 审稿服务）
// ============================================================

// CallExternalAPI 通用外部 API 调用
func (ctx *TaskContext) CallExternalAPI(apiURL, token string, body interface{}) (map[string]interface{}, error) {
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("序列化请求体失败: %w", err)
	}

	slog.Info("pipeline_external_call", "task_id", ctx.TaskID, "url", apiURL, "body", string(bodyBytes[:minInt(len(bodyBytes), 500)]))

	req, err := http.NewRequest(http.MethodPost, apiURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	start := time.Now()
	resp, err := callbackHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("调用外部接口失败: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	ctx.Timeline.RecordDuration("external_api", time.Since(start).Milliseconds(), 0)

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("外部接口返回 HTTP %d: %s", resp.StatusCode, string(respBody[:minInt(len(respBody), 500)]))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("解析外部接口响应失败: %w", err)
	}

	return result, nil
}