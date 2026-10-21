package handler

import (
	"encoding/json"
	"log/slog"
	"strconv"

	"doc-ai-gateway/internal/model"
	"doc-ai-gateway/internal/store"

	"github.com/gin-gonic/gin"
)

// InterfaceHandler 接口配置处理器
type InterfaceHandler struct {
	store store.MetaStore
}

// NewInterfaceHandler 创建处理器
func NewInterfaceHandler(st store.MetaStore) *InterfaceHandler {
	return &InterfaceHandler{store: st}
}

// List 列出所有接口
func (h *InterfaceHandler) List(c *gin.Context) {
	ifaces, err := h.store.ListInterfaces()
	if err != nil {
		c.JSON(500, gin.H{"error": "查询失败"})
		return
	}
	if ifaces == nil {
		ifaces = []model.InterfaceConfig{}
	}
	c.JSON(200, gin.H{"data": ifaces})
}

// Get 获取单个接口
func (h *InterfaceHandler) Get(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(400, gin.H{"error": "无效的 ID"})
		return
	}
	iface, err := h.store.GetInterfaceByID(id)
	if err != nil || iface == nil {
		c.JSON(404, gin.H{"error": "接口不存在"})
		return
	}
	c.JSON(200, gin.H{"data": iface})
}

// Create 创建接口
func (h *InterfaceHandler) Create(c *gin.Context) {
	var req model.InterfaceConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	if req.Name == "" || req.Path == "" || req.PromptTemplate == "" {
		c.JSON(400, gin.H{"error": "名称、路径和提示词模板不能为空"})
		return
	}

	// 检查路径唯一
	existing, _ := h.store.GetInterfaceByPath(req.Path)
	if existing != nil {
		c.JSON(409, gin.H{"error": "接口路径已存在"})
		return
	}

	if err := h.store.CreateInterface(&req); err != nil {
		c.JSON(500, gin.H{"error": "创建失败: " + err.Error()})
		return
	}
	slog.Info("admin_create_interface", "user", userName(c), "name", req.Name, "path", req.Path)
	c.JSON(200, gin.H{"status": "ok"})
}

// Update 更新接口
func (h *InterfaceHandler) Update(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(400, gin.H{"error": "无效的 ID"})
		return
	}

	var req model.InterfaceConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	req.ID = id
	if err := h.store.UpdateInterface(&req); err != nil {
		c.JSON(500, gin.H{"error": "更新失败: " + err.Error()})
		return
	}
	// 增加版本号
	h.store.IncrementInterfaceVersion(id)
	slog.Info("admin_update_interface", "user", userName(c), "id", id, "name", req.Name)
	c.JSON(200, gin.H{"status": "ok"})
}

// Delete 删除接口
func (h *InterfaceHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(400, gin.H{"error": "无效的 ID"})
		return
	}
	if err := h.store.DeleteInterface(id); err != nil {
		c.JSON(500, gin.H{"error": "删除失败"})
		return
	}
	slog.Info("admin_delete_interface", "user", userName(c), "id", id)
	c.JSON(200, gin.H{"status": "ok"})
}

// Test 测试接口（模拟调用，不实际调用 LLM）
func (h *InterfaceHandler) Test(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(400, gin.H{"error": "无效的 ID"})
		return
	}

	iface, err := h.store.GetInterfaceByID(id)
	if err != nil || iface == nil {
		c.JSON(404, gin.H{"error": "接口不存在"})
		return
	}

	// 返回渲染后的 prompt 用于调试
	var testParams map[string]interface{}
	c.ShouldBindJSON(&testParams)

	// 渲染模板
	rendered := renderPromptTemplate(iface.PromptTemplate, &model.FileRecord{
		OriginalName: "test.pdf",
		FileType:     "pdf",
		TextLength:   0,
		Status:       "ready",
	}, testParams)

	c.JSON(200, gin.H{
		"interface":     iface.Name,
		"path":          iface.Path,
		"rendered_prompt": rendered,
		"input_params":  iface.InputParams,
		"output_schema": iface.OutputSchema,
	})
}

// renderPromptTemplate 渲染提示词模板（简化版，用于测试接口展示）
func renderPromptTemplate(tmpl string, file *model.FileRecord, params map[string]interface{}) string {
	result := tmpl
	// 替换系统占位符
	if file != nil {
		result = replaceAll(result, "{{file.text}}", "[文件文本内容 - 预处理后自动填充]")
		result = replaceAll(result, "{{file.name}}", file.OriginalName)
		result = replaceAll(result, "{{file.type}}", file.FileType)
	}
	// 替换参数占位符
	for k, v := range params {
		placeholder := "{{params." + k + "}}"
		val, _ := json.Marshal(v)
		result = replaceAll(result, placeholder, string(val))
	}
	return result
}