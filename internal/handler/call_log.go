package handler

import (
	"doc-ai-gateway/internal/store"

	"github.com/gin-gonic/gin"
)

// CallLogHandler 调用日志处理器
type CallLogHandler struct {
	store store.MetaStore
}

// NewCallLogHandler 创建处理器
func NewCallLogHandler(st store.MetaStore) *CallLogHandler {
	return &CallLogHandler{store: st}
}

// List 列出调用日志
func (h *CallLogHandler) List(c *gin.Context) {
	limit := 50
	offset := 0
	fileID := c.Query("file_id")

	records, total, err := h.store.ListCallLogs(fileID, limit, offset)
	if err != nil {
		c.JSON(500, gin.H{"error": "查询失败"})
		return
	}

	c.JSON(200, gin.H{"data": records, "total": total})
}