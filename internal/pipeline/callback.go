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

	"doc_flow_agent/internal/model"
)

// callbackHTTPClient 回调专用 HTTP 客户端（跳过 TLS 证书验证，兼容自签证书）
var callbackHTTPClient = &http.Client{
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	},
	Timeout: 30 * time.Second,
}

// ============================================================
// callback 步骤：向 callback_url 发送结果（含重试）
// ============================================================

// CallbackResult 回调结果
type CallbackResult struct {
	Success     bool
	RetryCount  int
	FinalStatus string // completed / callback_fail
	ErrorMsg    string
	Body        string // 回调请求体 JSON
}

// DoCallback 执行回调并返回结果
// 由 handler/async_worker 在 Pipeline 完成后调用
func DoCallback(ctx *TaskContext, output *model.APIResponse) *CallbackResult {
	maxRetries := ctx.Cfg.Sys.CallbackRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}
	retryInterval := ctx.Cfg.Sys.CallbackRetryInterval
	if retryInterval <= 0 {
		retryInterval = 2
	}

	// 构造回调 body
	var bodyBytes []byte
	if ctx.Status == model.TaskStatusSuccess {
		bodyBytes, _ = json.Marshal(map[string]interface{}{
			"code":    model.ErrCodeSuccess,
			"message": "success",
			"task_id": ctx.TaskID,
			"linkid":  ctx.LinkID,
			"data":    output.Data,
		})
	} else {
		bodyBytes, _ = json.Marshal(map[string]interface{}{
			"code":    model.ErrCodeInternal,
			"message": ctx.ErrorMessage,
			"task_id": ctx.TaskID,
			"linkid":  ctx.LinkID,
			"data":    nil,
		})
	}

	result := &CallbackResult{Body: string(bodyBytes)}
	token := ctx.Cfg.Sys.CallbackToken

	var lastErr error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		cbStart := time.Now()
		req, err := http.NewRequest(http.MethodPost, ctx.CallbackURL, bytes.NewReader(bodyBytes))
		if err != nil {
			slog.Error("callback_req_create_failed", "task_id", ctx.TaskID, "error", err)
			lastErr = err
			break
		}
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}

		resp, err := callbackHTTPClient.Do(req)
		cbDur := time.Since(cbStart).Milliseconds()

		if err != nil {
			slog.Warn("callback_retry", "task_id", ctx.TaskID, "callback_url", ctx.CallbackURL, "error", err, "attempt", fmt.Sprintf("%d/%d", attempt, maxRetries))
			result.RetryCount = attempt
			ctx.Timeline.RecordDuration("callback", cbDur, attempt)
			lastErr = err
			if attempt < maxRetries {
				sleepSec := retryInterval * (1 << (attempt - 1))
				time.Sleep(time.Duration(sleepSec) * time.Second)
			}
			continue
		}

		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 500 && resp.StatusCode != 0 {
			ctx.Timeline.RecordDuration("callback", cbDur, attempt)
			result.Success = true
			result.RetryCount = attempt
			result.FinalStatus = model.TaskStatusCompleted
			slog.Info("callback_success", "task_id", ctx.TaskID, "callback_url", ctx.CallbackURL, "status", resp.StatusCode, "attempt", fmt.Sprintf("%d/%d", attempt, maxRetries))
			return result
		}

		slog.Warn("callback_retry", "task_id", ctx.TaskID, "callback_url", ctx.CallbackURL, "status", resp.StatusCode, "attempt", fmt.Sprintf("%d/%d", attempt, maxRetries))
		result.RetryCount = attempt
		ctx.Timeline.RecordDuration("callback", cbDur, attempt)
		lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
		if attempt < maxRetries {
			sleepSec := retryInterval * (1 << (attempt - 1))
			time.Sleep(time.Duration(sleepSec) * time.Second)
		}
	}

	result.Success = false
	result.RetryCount = maxRetries
	result.FinalStatus = model.TaskStatusCallbackFail
	result.ErrorMsg = fmt.Sprintf("回调失败（%d次重试耗尽，最后错误: %v）", maxRetries, lastErr)
	slog.Error("callback_exhausted", "task_id", ctx.TaskID, "callback_url", ctx.CallbackURL, "attempts", maxRetries, "response_body", result.Body, "last_error", lastErr)
	return result
}

// TaskContext status/error helpers (set by handler before callback)
// These exist on TaskContext but are set from outside pipeline in the actual flow;
// for now adding them as fields since they're used by callback.

// Status and ErrorMessage are additional context fields managed by handler/worker.
// They're read-only here; the handler sets them based on Pipeline.Run() result.