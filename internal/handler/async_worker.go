package handler

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
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

// process 处理单个异步任务
func (w *AsyncWorker) process(taskID string, workerIdx int) {
	startTime := time.Now()

	// 检查 DB 中任务是否还存在（可能被删除）
	task, err := w.store.GetAsyncTask(taskID)
	if err != nil || task == nil {
		slog.Warn("async_task_gone", "task_id", taskID, "worker", workerIdx)
		return
	}
	if task.Status != model.TaskStatusPending {
		return
	}
	linkid := task.LinkID

	slog.Info("async_task_start", "task_id", taskID, "linkid", linkid, "interface", task.InterfacePath, "worker", workerIdx)

	// 更新状态为 processing
	task.Status = model.TaskStatusProcessing
	w.store.UpdateAsyncTask(task)

	// 查找接口配置
	iface, err := w.store.GetInterfaceByPath(task.InterfacePath)
	if err != nil || iface == nil || !iface.Enabled {
		w.failTask(task, "接口不存在或已禁用: "+task.InterfacePath)
		return
	}

	// 解析请求体
	var rawBody map[string]interface{}
	if err := json.Unmarshal([]byte(task.RequestBody), &rawBody); err != nil {
		w.failTask(task, "解析请求体失败: "+err.Error())
		return
	}

	// 下载并解析文件
	files := w.callH.downloadAllFileURLs(rawBody, taskID)

	// 渲染 prompt
	systemPrompt := w.callH.buildSystemPrompt(iface)
	userPrompt, err := w.callH.renderPrompt(iface, task.RequestBody, files)
	if err != nil {
		w.failTask(task, err.Error())
		return
	}

	// 获取 LLM 配置
	llmCfg, err := w.callH.getLLMConfig(iface)
	if err != nil {
		w.failTask(task, err.Error())
		return
	}

	// 调用 LLM
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

	resultText, llmErr := client.Chat(systemPrompt, userPrompt, temperature, maxTokens)
	elapsedMs := time.Since(startTime).Milliseconds()

	// 记录调用日志
	logEntry := &model.CallLog{
		FileID:        task.TaskID, // 用 taskID 标识日志，方便全文过滤
		InterfaceID:   iface.ID,
		InterfacePath: task.InterfacePath,
		InputParams:   ellipsisJSON(task.RequestBody, 2000),
		ElapsedMs:     elapsedMs,
	}
	if llmErr != nil {
		logEntry.Status = "error"
		logEntry.ErrorMessage = llmErr.Error()
	} else {
		logEntry.Status = "success"
		logEntry.OutputResult = resultText
	}
	w.store.CreateCallLog(logEntry)

	// 更新任务状态
	task.ElapsedMs = elapsedMs

	if llmErr != nil {
		slog.Error("async_task_llm_error", "task_id", taskID, "linkid", linkid, "interface", task.InterfacePath, "error", llmErr, "elapsed_ms", elapsedMs)
		task.Status = model.TaskStatusFailed
		task.ErrorMessage = llmErr.Error()
	} else {
		output := w.callH.wrapOutput(resultText, iface.OutputSchema)
		task.Status = model.TaskStatusSuccess
		task.OutputResult = mustJSON(output)
	}
	w.store.UpdateAsyncTask(task)

	slog.Info("async_task_done", "task_id", taskID, "linkid", linkid, "status", task.Status, "elapsed_ms", elapsedMs, "worker", workerIdx)

	// 回调
	w.doCallback(task)
}

// failTask 标记任务失败
func (w *AsyncWorker) failTask(task *model.AsyncTask, errMsg string) {
	task.Status = model.TaskStatusFailed
	task.ErrorMessage = errMsg
	w.store.UpdateAsyncTask(task)
	slog.Error("async_task_failed", "task_id", task.TaskID, "linkid", task.LinkID, "error", errMsg)
	w.doCallback(task)
}

// doCallback 向 callback_url 发送结果
func (w *AsyncWorker) doCallback(task *model.AsyncTask) {
	maxRetries := w.cfg.Sys.CallbackRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}
	retryInterval := w.cfg.Sys.CallbackRetryInterval
	if retryInterval <= 0 {
		retryInterval = 2
	}

	// 构造回调 body（task_id 和 linkid 放在顶层，与受理响应结构一致）
	var body []byte
	if task.Status == model.TaskStatusSuccess {
		var output model.APIResponse
		json.Unmarshal([]byte(task.OutputResult), &output)
		body, _ = json.Marshal(map[string]interface{}{
			"code":    model.ErrCodeSuccess,
			"message": "success",
			"task_id": task.TaskID,
			"linkid":  task.LinkID,
			"data":    output.Data,
		})
	} else {
		body, _ = json.Marshal(map[string]interface{}{
			"code":    model.ErrCodeInternal,
			"message": task.ErrorMessage,
			"task_id": task.TaskID,
			"linkid":  task.LinkID,
			"data":    nil,
		})
	}

	token := w.cfg.Sys.CallbackToken

	for attempt := 1; attempt <= maxRetries; attempt++ {
		req, err := http.NewRequest(http.MethodPost, task.CallbackURL, bytes.NewReader(body))
		if err != nil {
			slog.Error("callback_req_create_failed", "task_id", task.TaskID, "linkid", task.LinkID, "error", err)
			break
		}
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}

		resp, err := callbackHTTPClient.Do(req)
		if err != nil {
			slog.Warn("callback_retry", "task_id", task.TaskID, "linkid", task.LinkID, "callback_url", task.CallbackURL, "error", err, "attempt", fmt.Sprintf("%d/%d", attempt, maxRetries))
			task.RetryCount = attempt
			w.store.UpdateAsyncTask(task)
			if attempt < maxRetries {
				sleepSec := retryInterval * (1 << (attempt - 1))
				time.Sleep(time.Duration(sleepSec) * time.Second)
				continue
			}
			break
		}

		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 500 && resp.StatusCode != 0 {
			task.Status = model.TaskStatusCompleted
			task.RetryCount = attempt
			w.store.UpdateAsyncTask(task)
			slog.Info("callback_success", "task_id", task.TaskID, "linkid", task.LinkID, "callback_url", task.CallbackURL, "status", resp.StatusCode, "attempt", fmt.Sprintf("%d/%d", attempt, maxRetries), "resp", ellipsisJSON(string(respBody), 500))
			return
		}

		slog.Warn("callback_retry", "task_id", task.TaskID, "linkid", task.LinkID, "callback_url", task.CallbackURL, "status", resp.StatusCode, "attempt", fmt.Sprintf("%d/%d", attempt, maxRetries), "next_in", fmt.Sprintf("%ds", retryInterval*(1<<(attempt-1))))
		task.RetryCount = attempt
		w.store.UpdateAsyncTask(task)
		if attempt < maxRetries {
			sleepSec := retryInterval * (1 << (attempt - 1))
			time.Sleep(time.Duration(sleepSec) * time.Second)
		}
	}

	// 重试耗尽
	task.Status = model.TaskStatusCallbackFail
	task.RetryCount = maxRetries
	w.store.UpdateAsyncTask(task)
	slog.Error("callback_exhausted", "task_id", task.TaskID, "linkid", task.LinkID, "callback_url", task.CallbackURL, "attempts", maxRetries, "response_body", string(body))
}