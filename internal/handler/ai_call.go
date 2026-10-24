package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"doc_flow_agent/internal/interfaces"
	"doc_flow_agent/internal/model"
	"doc_flow_agent/internal/store"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// CallHandler 通用调用处理器
type CallHandler struct {
	store  store.MetaStore
	cfg    *model.Config
	worker *AsyncWorker
}

// NewCallHandler 创建处理器
func NewCallHandler(st store.MetaStore, cfg *model.Config) *CallHandler {
	return &CallHandler{store: st, cfg: cfg}
}

// SetWorker 注入 AsyncWorker
func (h *CallHandler) SetWorker(w *AsyncWorker) {
	h.worker = w
}

// Call 通用异步入口
// POST /api/v1/*interfacePath
// 请求体：完整 JSON，必须含 linkid 和 callback_url
func (h *CallHandler) Call(c *gin.Context) {
	interfacePath := strings.TrimPrefix(c.Param("interfacePath"), "/")
	if interfacePath == "" || interfacePath == "/" {
		c.JSON(400, model.NewErrorResponse(model.ErrCodeParamError, "缺少接口路径"))
		return
	}

	// 读取请求体
	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(400, model.NewErrorResponse(model.ErrCodeParamError, "读取请求体失败"))
		return
	}
	if len(bodyBytes) == 0 {
		c.JSON(400, model.NewErrorResponse(model.ErrCodeParamError, "请求体为空"))
		return
	}

	var rawBody map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &rawBody); err != nil {
		c.JSON(400, model.NewErrorResponse(model.ErrCodeParamError, "请求体不是合法 JSON: "+err.Error()))
		return
	}

	slog.Info("call_request", "interface", interfacePath, "body", ellipsisJSON(string(bodyBytes), 2000))

	// 从接口注册表查找
	_, ok := interfaces.Registry[interfacePath]
	if !ok {
		c.JSON(404, model.NewErrorResponse(model.ErrCodeParamError, "接口不存在: "+interfacePath))
		return
	}

	// 提取 linkid
	linkid, _ := rawBody["linkid"].(string)
	if linkid == "" {
		c.JSON(400, ginResponse{Code: model.ErrCodeParamError, Message: "缺少 linkid", TaskID: ""})
		return
	}

	// 提取 callback_url
	callbackURL, _ := rawBody["callback_url"].(string)
	if callbackURL == "" {
		c.JSON(400, ginResponse{Code: model.ErrCodeParamError, Message: "缺少 callback_url", TaskID: ""})
		return
	}

	// 检查任务队列
	maxPending := h.cfg.Sys.MaxPendingTasks
	if maxPending <= 0 {
		maxPending = 10
	}
	running, err := h.store.CountRunningTasks()
	if err != nil {
		slog.Error("call_count_running_failed", "error", err)
		c.JSON(500, model.NewErrorResponse(model.ErrCodeInternal, "内部错误"))
		return
	}
	if running >= maxPending {
		maxConcurrent := h.cfg.Sys.MaxConcurrentTasks
		if maxConcurrent <= 0 {
			maxConcurrent = 5
		}
		msg := fmt.Sprintf("任务队列已满，当前处理中任务数 %d/%d（最大并发 %d，最大排队 %d），请稍后重试",
			running, maxPending, maxConcurrent, maxPending)
		c.JSON(503, ginResponse{Code: model.ErrCodeQueueFull, Message: msg, TaskID: ""})
		return
	}

	// 生成 task_id
	taskID := uuid.New().String()

	// 创建任务追踪记录
	task := &model.TaskTrace{
		TaskID:        taskID,
		LinkID:        linkid,
		InterfacePath: interfacePath,
		RequestBody:   string(bodyBytes),
		CallbackURL:   callbackURL,
		Status:        model.TaskStatusPending,
	}
	if err := h.store.CreateTaskTrace(task); err != nil {
		slog.Error("call_create_task_failed", "error", err, "task_id", taskID)
		c.JSON(500, ginResponse{Code: model.ErrCodeInternal, Message: "任务创建失败", TaskID: ""})
		return
	}

	// 入队（传 interfacePath，worker 从 Registry 查找接口定义）
	h.worker.Enqueue(taskID)

	slog.Info("call_accepted", "task_id", taskID, "linkid", linkid, "interface", interfacePath, "running", running+1, "max", maxPending)
	c.JSON(200, ginResponse{Code: model.ErrCodeSuccess, Message: "success", TaskID: taskID, LinkID: linkid})
}

// ginResponse 异步入口响应格式
type ginResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	TaskID  string `json:"task_id"`
	LinkID  string `json:"linkid,omitempty"`
}

// ellipsisJSON 截断 JSON 文本
func ellipsisJSON(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// basePath 返回文件存储基础路径
func (h *CallHandler) basePath() string {
	if h.cfg.Sys.BasePath != "" {
		return h.cfg.Sys.BasePath
	}
	return "data"
}