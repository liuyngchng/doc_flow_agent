package handler

import (
	"log/slog"

	"doc_flow_agent/internal/model"
	"doc_flow_agent/internal/store"

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

	apiAuthEnabled, _ := h.store.GetConfig("api_auth_enabled")
	apiAuthToken, _ := h.store.GetConfig("api_auth_token")

	c.JSON(200, gin.H{
		"data": gin.H{
			"sys": gin.H{
				"name": name,
			},
			"api": gin.H{
				"auth_enabled": apiAuthEnabled == "1",
				"auth_token":   apiAuthToken,
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
		API struct {
			AuthEnabled bool   `json:"auth_enabled"`
			AuthToken   string `json:"auth_token"`
		} `json:"api"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}

	if req.Sys.Name != "" {
		h.store.SetConfig("sys.name", req.Sys.Name, "系统名称")
		h.cfg.Sys.Name = req.Sys.Name
	}

	// API 认证配置
	h.store.SetConfig("api_auth_enabled", boolStr(req.API.AuthEnabled), "对外 API 是否启用 Bearer Token 认证")
	h.store.SetConfig("api_auth_token", req.API.AuthToken, "对外 API 认证 Token")

	slog.Info("admin_update_config", "user", userName(c), "sys_name", req.Sys.Name, "api_auth_enabled", req.API.AuthEnabled)

	c.JSON(200, gin.H{"status": "ok"})
}