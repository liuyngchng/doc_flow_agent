package handler

import (
	"encoding/json"
	"log/slog"
	"strconv"

	"doc_flow_agent/internal/model"
	"doc_flow_agent/internal/store"

	"github.com/gin-gonic/gin"
)

// LLMConfigHandler LLM 模型配置处理器
type LLMConfigHandler struct {
	store store.MetaStore
}

// NewLLMConfigHandler 创建处理器
func NewLLMConfigHandler(st store.MetaStore) *LLMConfigHandler {
	return &LLMConfigHandler{store: st}
}

// List 列出所有 LLM 配置
func (h *LLMConfigHandler) List(c *gin.Context) {
	configs, err := h.store.ListLLMConfigs()
	if err != nil {
		c.JSON(500, gin.H{"error": "查询失败"})
		return
	}
	if configs == nil {
		configs = []model.LLMConfig{}
	}
	c.JSON(200, gin.H{"data": configs})
}

// Create 创建 LLM 配置
func (h *LLMConfigHandler) Create(c *gin.Context) {
	var req model.LLMConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	if req.Name == "" || req.APIURI == "" || req.ModelName == "" {
		c.JSON(400, gin.H{"error": "名称、API地址和模型名称不能为空"})
		return
	}
	if err := h.store.CreateLLMConfig(&req); err != nil {
		c.JSON(500, gin.H{"error": "创建失败: " + err.Error()})
		return
	}
	slog.Info("admin_create_llm", "user", userName(c), "name", req.Name, "model", req.ModelName)
	c.JSON(200, gin.H{"status": "ok"})
}

// Update 更新 LLM 配置
func (h *LLMConfigHandler) Update(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(400, gin.H{"error": "无效的 ID"})
		return
	}

	var req model.LLMConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	req.ID = id
	if err := h.store.UpdateLLMConfig(&req); err != nil {
		c.JSON(500, gin.H{"error": "更新失败: " + err.Error()})
		return
	}
	slog.Info("admin_update_llm", "user", userName(c), "id", id, "name", req.Name)
	c.JSON(200, gin.H{"status": "ok"})
}

// Delete 删除 LLM 配置
func (h *LLMConfigHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(400, gin.H{"error": "无效的 ID"})
		return
	}
	if err := h.store.DeleteLLMConfig(id); err != nil {
		c.JSON(500, gin.H{"error": "删除失败"})
		return
	}
	slog.Info("admin_delete_llm", "user", userName(c), "id", id)
	c.JSON(200, gin.H{"status": "ok"})
}

// SetDefault 设置默认 LLM
func (h *LLMConfigHandler) SetDefault(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(400, gin.H{"error": "无效的 ID"})
		return
	}
	if err := h.store.SetDefaultLLMConfig(id); err != nil {
		c.JSON(500, gin.H{"error": "设置失败"})
		return
	}
	c.JSON(200, gin.H{"status": "ok"})
}

// Test 测试 LLM 连接
func (h *LLMConfigHandler) Test(c *gin.Context) {
	var req struct {
		APIURI    string `json:"api_uri"`
		APIKey    string `json:"api_key"`
		ModelName string `json:"model_name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}

	// 保存测试参数为 JSON 供调用接口使用
	paramsJSON, _ := json.Marshal(req)

	c.JSON(200, gin.H{
		"message": "测试连接请通过 /api/inner/llm-configs/test-call 接口",
		"params":  string(paramsJSON),
	})
}