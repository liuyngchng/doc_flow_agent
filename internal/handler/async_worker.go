package handler

import (
	"encoding/json"
	"log/slog"
	"path/filepath"
	"time"

	"doc_flow_agent/internal/interfaces"
	"doc_flow_agent/internal/model"
	"doc_flow_agent/internal/pipeline"
	"doc_flow_agent/internal/store"
)

// AsyncWorker 异步任务工作池
type AsyncWorker struct {
	store    store.MetaStore
	cfg      *model.Config
	taskChan chan string
	stopCh   chan struct{}
}

// NewAsyncWorker 创建工作池
func NewAsyncWorker(st store.MetaStore, cfg *model.Config) *AsyncWorker {
	bufSize := cfg.Sys.MaxPendingTasks
	if bufSize <= 0 {
		bufSize = 10
	}
	return &AsyncWorker{
		store:    st,
		cfg:      cfg,
		taskChan: make(chan string, bufSize),
		stopCh:   make(chan struct{}),
	}
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

// Stop 停止 Worker
func (w *AsyncWorker) Stop() {
	close(w.stopCh)
	slog.Info("async_worker_stopping")
}

// Enqueue 将 taskID 放入 channel
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

// process 处理单个异步任务 — 使用 Pipeline
func (w *AsyncWorker) process(taskID string, workerIdx int) {
	// 检查 DB 中任务是否存在
	trace, err := w.store.GetTaskTrace(taskID)
	if err != nil || trace == nil {
		slog.Warn("task_gone", "task_id", taskID, "worker", workerIdx)
		return
	}
	if trace.Status != model.TaskStatusPending {
		return
	}

	slog.Info("task_start", "task_id", taskID, "linkid", trace.LinkID, "interface", trace.InterfacePath, "worker", workerIdx)

	// 更新状态为 processing
	trace.Status = model.TaskStatusProcessing
	timeline := pipeline.NewTimeline()
	timeline.Record("process_start")
	trace.Timeline = timeline.Marshal()
	w.store.UpdateTaskTrace(trace)

	// 从注册表查找接口定义
	def := interfaces.Registry[trace.InterfacePath]
	if def == nil {
		w.failTrace(trace, "接口不存在: "+trace.InterfacePath, timeline)
		return
	}

	// 解析请求体
	var rawBody map[string]interface{}
	if err := json.Unmarshal([]byte(trace.RequestBody), &rawBody); err != nil {
		w.failTrace(trace, "解析请求体失败: "+err.Error(), timeline)
		return
	}

	// 构建 Pipeline 上下文
	ctx := &pipeline.TaskContext{
		TaskID:      taskID,
		LinkID:      trace.LinkID,
		Interface:   def,
		RawBody:     rawBody,
		RawBodyStr:  trace.RequestBody,
		CallbackURL: trace.CallbackURL,
		Store:       w.store,
		Cfg:         w.cfg,
		Trace:       trace,
		Timeline:    timeline,
	}

	// 加载模板
	if err := ctx.LoadTemplate(); err != nil {
		w.failTrace(trace, err.Error(), timeline)
		return
	}

	// 执行 Pipeline
	if err := pipeline.Run(ctx); err != nil {
		slog.Error("task_pipeline_error", "task_id", taskID, "linkid", trace.LinkID, "interface", trace.InterfacePath, "error", err)
		ctx.Status = model.TaskStatusFailed
		ctx.ErrorMessage = err.Error()
		trace.Status = model.TaskStatusFailed
		trace.ErrorMessage = err.Error()
		trace.Timeline = timeline.Marshal()
		trace.FinishedAt = nowISO()
		w.store.UpdateTaskTrace(trace)
		w.doCallback(ctx, nil)
		return
	}

	// Pipeline 成功 → 包装输出
	output := pipeline.WrapOutput(ctx.LLMRawOutput, ctx.Template.OutputSchema)
	ctx.OutputResult = &output

	// 更新 trace
	trace.Status = model.TaskStatusSuccess
	trace.OutputResult = mustJSON(output)
	trace.LLMRequestPrompt = ctx.SystemPrompt + "\n\n" + ctx.RenderedPrompt
	trace.LLMRawOutput = ctx.LLMRawOutput
	trace.Timeline = timeline.Marshal()
	trace.FinishedAt = nowISO()

	// 文件信息落库
	if ctx.DownloadResult != nil {
		filesText, filesSummary := buildFilesTextAndSummary(ctx.DownloadResult)
		trace.FilesText = mustJSON(filesText)
		trace.FilesSummary = mustJSON(filesSummary)
		if fileErrs := collectFileErrors(filesSummary); len(fileErrs) > 0 {
			trace.ErrorMessage = "部分文件处理失败: " + fileErrs[0]
			slog.Warn("task_file_errors", "task_id", taskID, "errors", fileErrs)
		}
	}

	w.store.UpdateTaskTrace(trace)

	ctx.Status = model.TaskStatusSuccess
	slog.Info("task_done", "task_id", taskID, "linkid", trace.LinkID, "status", trace.Status, "worker", workerIdx)

	w.doCallback(ctx, &output)
}

// failTrace 标记任务失败
func (w *AsyncWorker) failTrace(trace *model.TaskTrace, errMsg string, tl *pipeline.TimelineRecorder) {
	trace.Status = model.TaskStatusFailed
	trace.ErrorMessage = errMsg
	trace.Timeline = tl.Marshal()
	trace.FinishedAt = nowISO()
	w.store.UpdateTaskTrace(trace)
	slog.Error("task_failed", "task_id", trace.TaskID, "linkid", trace.LinkID, "error", errMsg)
}

// doCallback 向 callback_url 发送结果
func (w *AsyncWorker) doCallback(ctx *pipeline.TaskContext, output *model.APIResponse) {
	result := pipeline.DoCallback(ctx, output)

	// 更新 trace
	ctx.Trace.CallbackBody = result.Body
	ctx.Trace.RetryCount = result.RetryCount
	ctx.Trace.Timeline = ctx.Timeline.Marshal()
	if result.FinalStatus != "" {
		ctx.Trace.Status = result.FinalStatus
	}
	if result.ErrorMsg != "" && ctx.Trace.ErrorMessage == "" {
		ctx.Trace.ErrorMessage = result.ErrorMsg
	}
	w.store.UpdateTaskTrace(ctx.Trace)
}

// buildFilesTextAndSummary 构造文件全文和摘要
func buildFilesTextAndSummary(files *model.DownloadResult) ([]model.FileTextItem, []model.FileSummaryItem) {
	if files == nil || len(files.FlatItems) == 0 {
		return nil, nil
	}

	var texts []model.FileTextItem
	var summaries []model.FileSummaryItem

	for _, item := range files.FlatItems {
		ext := filepath.Ext(item.FileName)
		format := ""
		if ext != "" {
			format = ext[1:]
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

// collectFileErrors 提取文件层失败信息
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

// nowISO 返回当前 ISO8601 时间
func nowISO() string {
	return time.Now().Format("2006-01-02T15:04:05.000-07:00")
}

// mustJSON 序列化 JSON，失败返回 "{}"
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