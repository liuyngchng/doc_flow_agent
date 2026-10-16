package handler

import (
	"doc-ai-gateway/internal/model"
	"doc-ai-gateway/internal/store"

	"github.com/gin-gonic/gin"
)

// ConfigHandler 系统配置处理器
type ConfigHandler struct {
	cfg   *model.Config
	store store.MetaStore
}

// NewConfigHandler 创建配置处理器
func NewConfigHandler(cfg *model.Config, st store.MetaStore) *ConfigHandler {
	return &ConfigHandler{cfg: cfg, store: st}
}

// GetConfig 获取配置
func (h *ConfigHandler) GetConfig(c *gin.Context) {
	name, _ := h.store.GetConfig("sys.name")
	if name == "" {
		name = h.cfg.Sys.Name
	}

	c.JSON(200, gin.H{
		"data": gin.H{
			"sys": gin.H{
				"name": name,
			},
		},
	})
}

// UpdateConfig 更新配置
func (h *ConfigHandler) UpdateConfig(c *gin.Context) {
	var req struct {
		Sys struct {
			Name string `json:"name"`
		} `json:"sys"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}

	if req.Sys.Name != "" {
		h.store.SetConfig("sys.name", req.Sys.Name, "系统名称")
		h.cfg.Sys.Name = req.Sys.Name
	}

	c.JSON(200, gin.H{"status": "ok"})
}