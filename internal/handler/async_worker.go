package handler

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"doc_flow_agent/internal/llm"
	"doc_flow_agent/internal/model"
	"doc_flow_agent/internal/store"
)

// AsyncWorker 异步任务工作池
type AsyncWorker struct {
	store    store.MetaStore
	cfg      *model.Config
	callH    *CallHandler
	taskChan chan string // channel 里放 taskID
	stopCh   chan struct{}
}

// NewAsyncWorker 创建工作池
func NewAsyncWorker(st store.MetaStore, cfg *model.Config) *AsyncWorker {
	bufSize := cfg.Sys.MaxPendingTasks
	if bufSize <= 0 {
		bufSize = 10
	}
	w := &AsyncWorker{
		store:    st,
		cfg:      cfg,
		callH:    NewCallHandler(st, cfg),
		taskChan: make(chan string, bufSize),
		stopCh:   make(chan struct{}),
	}
	return w
}

// callbackHTTPClient 回调专用 HTTP 客户端（跳过 TLS 证书验证，兼容自签证书）
var callbackHTTPClient = &http.Client{
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	},
	Timeout: 30 * time.Second,
}

// Start 启动 Worker goroutines
func (w *AsyncWorker) Start() {
	workerCount := w.cfg.Sys.MaxConcurrentTasks
	if workerCount <= 0 {
		workerCount = 5
	}
	for i := 0; i < workerCount; i++ {
		go w.worker(i)
	}
	slog.Info("async_worker_started", "workers", workerCount, "max_pending", w.cfg.Sys.MaxPendingTasks)
}

// Stop 停止 Worker（等待当前处理中的任务完成）
func (w *AsyncWorker) Stop() {
	close(w.stopCh)
	slog.Info("async_worker_stopping")
}

// Enqueue 将 taskID 放入 channel 队列
func (w *AsyncWorker) Enqueue(taskID string) {
	w.taskChan <- taskID
}

// worker 工作 goroutine
func (w *AsyncWorker) worker(idx int) {
	slog.Info("async_worker_running", "worker", idx)
	for {
		select {
		case <-w.stopCh:
			slog.Info("async_worker_exit", "worker", idx)
			return
		case taskID := <-w.taskChan:
			w.process(taskID, idx)
		}
	}
}

// -------------------------------- timeline helpers --------------------------------

func nowISO() string {
	return time.Now().Format("2006-01-02T15:04:05.000-07:00")
}

func newTimeline(step string) []model.TimelineEntry {
	return []model.TimelineEntry{{Step: step, At: nowISO()}}
}

func appendTimeline(existing []model.TimelineEntry, step string, durMs int64, attempt int) []model.TimelineEntry {
	e := model.TimelineEntry{Step: step, At: nowISO(), DurationMs: durMs}
	if attempt > 0 {
		e.Attempt = attempt
	}
	return append(existing, e)
}

func marshalTimeline(tl []model.TimelineEntry) string {
	b, _ := json.Marshal(tl)
	return string(b)
}

// --------------------------------------------------------------------------------

// process 处理单个异步任务
func (w *AsyncWorker) process(taskID string, workerIdx int) {
	// 检查 DB 中任务是否还存在
	trace, err := w.store.GetTaskTrace(taskID)
	if err != nil || trace == nil {
		slog.Warn("task_gone", "task_id", taskID, "worker", workerIdx)
		return
	}
	if trace.Status != model.TaskStatusPending {
		return
	}

	linkid := trace.LinkID
	slog.Info("task_start", "task_id", taskID, "linkid", linkid, "interface", trace.InterfacePath, "worker", workerIdx)

	// 更新状态为 processing，追加 process_start
	var tl []model.TimelineEntry
	if trace.Timeline != "" {
		json.Unmarshal([]byte(trace.Timeline), &tl)
	}
	tl = appendTimeline(tl, "process_start", 0, 0)
	trace.Status = model.TaskStatusProcessing
	trace.Timeline = marshalTimeline(tl)
	w.store.UpdateTaskTrace(trace)

	// 查找接口配置
	iface, err := w.store.GetInterfaceByPath(trace.InterfacePath)
	if err != nil || iface == nil || !iface.Enabled {
		w.failTrace(trace, "接口不存在或已禁用: "+trace.InterfacePath, tl)
		return
	}

	// 解析请求体
	var rawBody map[string]interface{}
	if err := json.Unmarshal([]byte(trace.RequestBody), &rawBody); err != nil {
		w.failTrace(trace, "解析请求体失败: "+err.Error(), tl)
		return
	}

	// ---- download -----------------------------------------------------------
	dlStart := time.Now()
	files := w.callH.downloadAllFileURLs(rawBody, taskID)
	dlDur := time.Since(dlStart).Milliseconds()

	// 构造 files_text（含全文）+ files_summary（轻量摘要）
	filesText, filesSummary := buildFilesTextAndSummary(files)
	trace.FilesText = mustJSON(filesText)
	trace.FilesSummary = mustJSON(filesSummary)

	// 收集文件层失败信息（不阻断任务，仅记录警告）
	if fileErrs := collectFileErrors(filesSummary); len(fileErrs) > 0 {
		trace.ErrorMessage = "部分文件处理失败: " + strings.Join(fileErrs, "; ")
		slog.Warn("task_file_errors", "task_id", taskID, "errors", trace.ErrorMessage)
	}

	tl = appendTimeline(tl, "download", dlDur, 0)
	// ---- download done ------------------------------------------------------

	// 渲染 prompt
	systemPrompt := w.callH.buildSystemPrompt(iface)
	userPrompt, err := w.callH.renderPrompt(iface, trace.RequestBody, files)
	if err != nil {
		w.failTrace(trace, err.Error(), tl)
		return
	}
	trace.LLMRequestPrompt = userPrompt

	// 获取 LLM 配置
	llmCfg, err := w.callH.getLLMConfig(iface)
	if err != nil {
		w.failTrace(trace, err.Error(), tl)
		return
	}

	// ---- llm ----------------------------------------------------------------
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

	llmStart := time.Now()
	resultText, llmErr := client.Chat(systemPrompt, userPrompt, temperature, maxTokens)
	llmDur := time.Since(llmStart).Milliseconds()

	trace.LLMRawOutput = resultText
	tl = appendTimeline(tl, "llm", llmDur, 0)
	// ---- llm done -----------------------------------------------------------

	if llmErr != nil {
		slog.Error("task_llm_error", "task_id", taskID, "linkid", linkid, "interface", trace.InterfacePath, "error", llmErr)
		trace.ErrorMessage = llmErr.Error()
		trace.Status = model.TaskStatusFailed
		trace.Timeline = marshalTimeline(tl)
		trace.FinishedAt = nowISO()
		w.store.UpdateTaskTrace(trace)
		w.doCallback(trace)
		return
	}

	// 包装输出
	output := w.callH.wrapOutput(resultText, iface.OutputSchema)
	trace.Status = model.TaskStatusSuccess
	trace.OutputResult = mustJSON(output)
	trace.FinishedAt = nowISO()
	trace.Timeline = marshalTimeline(tl)
	w.store.UpdateTaskTrace(trace)

	slog.Info("task_done", "task_id", taskID, "linkid", linkid, "status", trace.Status, "worker", workerIdx)

	// 回调
	w.doCallback(trace)
}

// failTrace 标记任务失败，落库 + 回调
func (w *AsyncWorker) failTrace(trace *model.TaskTrace, errMsg string, tl []model.TimelineEntry) {
	trace.Status = model.TaskStatusFailed
	trace.ErrorMessage = errMsg
	trace.Timeline = marshalTimeline(tl)
	trace.FinishedAt = nowISO()
	w.store.UpdateTaskTrace(trace)
	slog.Error("task_failed", "task_id", trace.TaskID, "linkid", trace.LinkID, "error", errMsg)
	w.doCallback(trace)
}

// doCallback 向 callback_url 发送结果
func (w *AsyncWorker) doCallback(trace *model.TaskTrace) {
	maxRetries := w.cfg.Sys.CallbackRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}
	retryInterval := w.cfg.Sys.CallbackRetryInterval
	if retryInterval <= 0 {
		retryInterval = 2
	}

	// 构造回调 body
	var body []byte
	if trace.Status == model.TaskStatusSuccess {
		var output model.APIResponse
		json.Unmarshal([]byte(trace.OutputResult), &output)
		body, _ = json.Marshal(map[string]interface{}{
			"code":    model.ErrCodeSuccess,
			"message": "success",
			"task_id": trace.TaskID,
			"linkid":  trace.LinkID,
			"data":    output.Data,
		})
	} else {
		body, _ = json.Marshal(map[string]interface{}{
			"code":    model.ErrCodeInternal,
			"message": trace.ErrorMessage,
			"task_id": trace.TaskID,
			"linkid":  trace.LinkID,
			"data":    nil,
		})
	}

	trace.CallbackBody = string(body)

	// 载入已有 timeline
	var tl []model.TimelineEntry
	if trace.Timeline != "" {
		json.Unmarshal([]byte(trace.Timeline), &tl)
	}

	token := w.cfg.Sys.CallbackToken

	var lastErr error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		cbStart := time.Now()
		req, err := http.NewRequest(http.MethodPost, trace.CallbackURL, bytes.NewReader(body))
		if err != nil {
			slog.Error("callback_req_create_failed", "task_id", trace.TaskID, "linkid", trace.LinkID, "error", err)
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
			slog.Warn("callback_retry", "task_id", trace.TaskID, "linkid", trace.LinkID, "callback_url", trace.CallbackURL, "error", err, "attempt", fmt.Sprintf("%d/%d", attempt, maxRetries))
			trace.RetryCount = attempt
			// 记录本次回调尝试的时间线
			tl = appendTimeline(tl, "callback", cbDur, attempt)
			trace.Timeline = marshalTimeline(tl)
			w.store.UpdateTaskTrace(trace)

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
			tl = appendTimeline(tl, "callback", cbDur, attempt)
			trace.Status = model.TaskStatusCompleted
			trace.RetryCount = attempt
			trace.Timeline = marshalTimeline(tl)
			w.store.UpdateTaskTrace(trace)
			slog.Info("callback_success", "task_id", trace.TaskID, "linkid", trace.LinkID, "callback_url", trace.CallbackURL, "status", resp.StatusCode, "attempt", fmt.Sprintf("%d/%d", attempt, maxRetries), "resp", ellipsisJSON(string(respBody), 500))
			return
		}

		slog.Warn("callback_retry", "task_id", trace.TaskID, "linkid", trace.LinkID, "callback_url", trace.CallbackURL, "status", resp.StatusCode, "attempt", fmt.Sprintf("%d/%d", attempt, maxRetries), "next_in", fmt.Sprintf("%ds", retryInterval*(1<<(attempt-1))))
		trace.RetryCount = attempt
		tl = appendTimeline(tl, "callback", cbDur, attempt)
		trace.Timeline = marshalTimeline(tl)
		w.store.UpdateTaskTrace(trace)
		lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
		if attempt < maxRetries {
			sleepSec := retryInterval * (1 << (attempt - 1))
			time.Sleep(time.Duration(sleepSec) * time.Second)
		}
	}

	// 重试耗尽
	trace.Status = model.TaskStatusCallbackFail
	trace.RetryCount = maxRetries
	trace.ErrorMessage = fmt.Sprintf("回调失败（%d次重试耗尽，最后错误: %v）", maxRetries, lastErr)
	trace.Timeline = marshalTimeline(tl)
	w.store.UpdateTaskTrace(trace)
	slog.Error("callback_exhausted", "task_id", trace.TaskID, "linkid", trace.LinkID, "callback_url", trace.CallbackURL, "attempts", maxRetries, "response_body", string(body), "last_error", lastErr)
}

// buildFilesTextAndSummary 构造 files_text（全文+元信息）和 files_summary（轻量摘要）
func buildFilesTextAndSummary(files *downloadedFiles) ([]model.FileTextItem, []model.FileSummaryItem) {
	if files == nil || len(files.FlatItems) == 0 {
		return nil, nil
	}

	var texts []model.FileTextItem
	var summaries []model.FileSummaryItem

	for _, item := range files.FlatItems {
		ext := filepath.Ext(item.FileName)
		format := ""
		if ext != "" {
			format = ext[1:] // 去掉 .
		}

		textItem := model.FileTextItem{
			FileURL:   item.URL,
			FileName:  item.FileName,
			FileType:  item.FileType,
			Format:    format,
			LocalPath: item.LocalPath,
			HasSeal:   item.HasSeal,
			SealText:  item.SealTexts,
			TextLen:   len(item.Text),
			Text:      item.Text,
		}
		texts = append(texts, textItem)

		summaryItem := model.FileSummaryItem{
			FileURL:    item.URL,
			FileName:   item.FileName,
			FileType:   item.FileType,
			Format:     format,
			TextLen:    len(item.Text),
			HasSeal:    item.HasSeal,
			Status:     item.Status,
			ErrorMsg:   item.ErrorMsg,
			DurationMs: item.DurationMs,
		}
		summaries = append(summaries, summaryItem)
	}

	return texts, summaries
}

// collectFileErrors 从文件摘要中提取所有失败项，返回「文件名: 原因」列表
func collectFileErrors(summaries []model.FileSummaryItem) []string {
	var errs []string
	for _, s := range summaries {
		if s.Status == "ok" {
			continue
		}
		name := s.FileName
		if name == "" {
			name = s.FileURL
		}
		reason := s.ErrorMsg
		if reason == "" {
			reason = s.Status
		}
		errs = append(errs, name+": "+reason)
	}
	return errs
}
