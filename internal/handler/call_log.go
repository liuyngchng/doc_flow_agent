package handler

import (
	"doc_flow_agent/internal/store"

	"github.com/gin-gonic/gin"
)

// TaskTraceHandler 任务追踪处理器
type TaskTraceHandler struct {
	store store.MetaStore
}

// NewTaskTraceHandler 创建处理器
func NewTaskTraceHandler(st store.MetaStore) *TaskTraceHandler {
	return &TaskTraceHandler{store: st}
}

// List 列出任务追踪记录（最近 10 条）
func (h *TaskTraceHandler) List(c *gin.Context) {
	records, total, err := h.store.ListTaskTraces(10, 0)
	if err != nil {
		c.JSON(500, gin.H{"error": "查询失败"})
		return
	}

	c.JSON(200, gin.H{"data": records, "total": total})
}

// Get 获取单条任务追踪详情
func (h *TaskTraceHandler) Get(c *gin.Context) {
	taskID := c.Param("task_id")
	if taskID == "" {
		c.JSON(400, gin.H{"error": "缺少 task_id"})
		return
	}
	record, err := h.store.GetTaskTrace(taskID)
	if err != nil || record == nil {
		c.JSON(404, gin.H{"error": "任务不存在"})
		return
	}
	c.JSON(200, gin.H{"data": record})
}