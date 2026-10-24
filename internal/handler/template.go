package handler

import (
	"log/slog"
	"strconv"

	"doc_flow_agent/internal/interfaces"
	"doc_flow_agent/internal/model"
	"doc_flow_agent/internal/store"

	"github.com/gin-gonic/gin"
)

// TemplateHandler 提示词模板处理器
type TemplateHandler struct {
	store store.MetaStore
}

// NewTemplateHandler 创建处理器
func NewTemplateHandler(st store.MetaStore) *TemplateHandler {
	return &TemplateHandler{store: st}
}

// List 列出所有模板
func (h *TemplateHandler) List(c *gin.Context) {
	templates, err := h.store.ListTemplates()
	if err != nil {
		c.JSON(500, gin.H{"error": "查询失败"})
		return
	}
	if templates == nil {
		templates = []model.TemplateConfig{}
	}
	c.JSON(200, gin.H{"data": templates})
}

// Get 获取单个模板
func (h *TemplateHandler) Get(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(400, gin.H{"error": "无效的 ID"})
		return
	}
	tmpl, err := h.store.GetTemplateByID(id)
	if err != nil || tmpl == nil {
		c.JSON(404, gin.H{"error": "模板不存在"})
		return
	}
	c.JSON(200, gin.H{"data": tmpl})
}

// Create 创建模板
func (h *TemplateHandler) Create(c *gin.Context) {
	var req model.TemplateConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	if req.Name == "" || req.PromptTemplate == "" {
		c.JSON(400, gin.H{"error": "名称和提示词模板不能为空"})
		return
	}
	if err := h.store.CreateTemplate(&req); err != nil {
		c.JSON(500, gin.H{"error": "创建失败: " + err.Error()})
		return
	}
	slog.Info("admin_create_template", "user", userName(c), "name", req.Name)
	c.JSON(200, gin.H{"status": "ok"})
}

// Update 更新模板
func (h *TemplateHandler) Update(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(400, gin.H{"error": "无效的 ID"})
		return
	}
	var req model.TemplateConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	req.ID = id
	if err := h.store.UpdateTemplate(&req); err != nil {
		c.JSON(500, gin.H{"error": "更新失败: " + err.Error()})
		return
	}
	h.store.IncrementTemplateVersion(id)
	slog.Info("admin_update_template", "user", userName(c), "id", id, "name", req.Name)
	c.JSON(200, gin.H{"status": "ok"})
}

// Delete 删除模板
func (h *TemplateHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(400, gin.H{"error": "无效的 ID"})
		return
	}
	if err := h.store.DeleteTemplate(id); err != nil {
		c.JSON(500, gin.H{"error": "删除失败"})
		return
	}
	slog.Info("admin_delete_template", "user", userName(c), "id", id)
	c.JSON(200, gin.H{"status": "ok"})
}

// ListInterfaces 返回代码中注册的所有接口定义（供前端模板创建时选择）
func (h *TemplateHandler) ListInterfaces(c *gin.Context) {
	defs := interfaces.List()
	c.JSON(200, gin.H{"data": defs})
}