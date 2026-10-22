package handler

import (
	"net/http"

	"doc_flow_agent/internal/model"

	"github.com/gin-gonic/gin"
)

// PageHandler 页面处理器
type PageHandler struct {
	cfg *model.Config
}

// NewPageHandler 创建页面处理器
func NewPageHandler(cfg *model.Config) *PageHandler {
	return &PageHandler{cfg: cfg}
}

// AdminIndex 管理后台主页
func (h *PageHandler) AdminIndex(c *gin.Context) {
	uid, role := GetUserInfo(c)

	c.HTML(http.StatusOK, "admin.html", gin.H{
		"sys_name":   h.cfg.Sys.Name,
		"page_title": h.cfg.Sys.Name + " - 系统管理",
		"uid":        uid,
		"role":       role,
	})
}

// RegisterPage 注册页
func (h *PageHandler) RegisterPage(c *gin.Context) {
	c.HTML(http.StatusOK, "register.html", gin.H{
		"page_title": h.cfg.Sys.Name,
	})
}