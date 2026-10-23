package handler

import (
	"log/slog"
	"net/http"

	"doc_flow_agent/internal/model"
	"doc_flow_agent/internal/store"

	"github.com/gin-gonic/gin"
)

// AsyncTaskHandler 异步任务管理处理器
type AsyncTaskHandler struct {
	store store.MetaStore
	cfg   *model.Config
}

// NewAsyncTaskHandler 创建处理器
func NewAsyncTaskHandler(st store.MetaStore, cfg *model.Config) *AsyncTaskHandler {
	return &AsyncTaskHandler{store: st, cfg: cfg}
}

// ListTasks GET /api/v1/tasks — 查询进行中任务
func (h *AsyncTaskHandler) ListTasks(c *gin.Context) {
	tasks, err := h.store.ListRunningTasks()
	if err != nil {
		slog.Error("async_list_tasks_failed", "error", err)
		c.JSON(http.StatusInternalServerError, model.NewErrorResponse(model.ErrCodeInternal, "查询任务失败"))
		return
	}

	maxConcurrent := h.cfg.Sys.MaxConcurrentTasks
	if maxConcurrent <= 0 {
		maxConcurrent = 5
	}
	maxPending := h.cfg.Sys.MaxPendingTasks
	if maxPending <= 0 {
		maxPending = 10
	}

	type taskItem struct {
		TaskID        string `json:"task_id"`
		LinkID        string `json:"linkid"`
		InterfacePath string `json:"interface_path"`
		Status        string `json:"status"`
		CallbackURL   string `json:"callback_url"`
		ElapsedMs     int64  `json:"elapsed_ms"`
		CreatedAt     string `json:"created_at"`
	}

	items := make([]taskItem, 0, len(tasks))
	for _, t := range tasks {
		items = append(items, taskItem{
			TaskID:        t.TaskID,
			LinkID:        t.LinkID,
			InterfacePath: t.InterfacePath,
			Status:        t.Status,
			CallbackURL:   t.CallbackURL,
			ElapsedMs:     t.ElapsedMs,
			CreatedAt:     t.CreatedAt,
		})
	}

	c.JSON(http.StatusOK, model.NewSuccessResponse(map[string]interface{}{
		"total":          len(tasks),
		"max_concurrent": maxConcurrent,
		"max_pending":    maxPending,
		"tasks":          items,
	}))
}

// DeleteTask DELETE /api/v1/tasks/:task_id — 取消排队中任务
func (h *AsyncTaskHandler) DeleteTask(c *gin.Context) {
	taskID := c.Param("task_id")
	if taskID == "" {
		c.JSON(http.StatusBadRequest, ginResponse{Code: model.ErrCodeParamError, Message: "缺少 task_id", TaskID: ""})
		return
	}

	task, err := h.store.GetAsyncTask(taskID)
	if err != nil {
		slog.Error("async_delete_task_failed", "error", err, "task_id", taskID)
		c.JSON(http.StatusInternalServerError, ginResponse{Code: model.ErrCodeInternal, Message: "查询任务失败", TaskID: ""})
		return
	}
	if task == nil {
		c.JSON(http.StatusOK, ginResponse{Code: model.ErrCodeParamError, Message: "任务不存在或已结束", TaskID: ""})
		return
	}

	switch task.Status {
	case model.TaskStatusPending:
		if err := h.store.DeleteAsyncTask(taskID); err != nil {
			slog.Error("async_delete_task_failed", "error", err, "task_id", taskID)
			c.JSON(http.StatusInternalServerError, ginResponse{Code: model.ErrCodeInternal, Message: "删除任务失败", TaskID: ""})
			return
		}
		slog.Info("async_task_cancelled", "task_id", taskID)
		c.JSON(http.StatusOK, ginResponse{Code: model.ErrCodeSuccess, Message: "任务已取消", TaskID: taskID})

	case model.TaskStatusProcessing:
		c.JSON(http.StatusOK, ginResponse{Code: model.ErrCodeParamError, Message: "任务正在处理中，无法取消", TaskID: taskID})

	default:
		c.JSON(http.StatusOK, ginResponse{Code: model.ErrCodeParamError, Message: "任务不存在或已结束", TaskID: ""})
	}
}