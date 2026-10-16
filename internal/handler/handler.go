package handler

import (
	"doc-ai-gateway/internal/model"
	"doc-ai-gateway/internal/store"
)

// Handler 聚合所有处理器
type Handler struct {
	Config     *ConfigHandler
	Auth       *AuthHandler
	Page       *PageHandler
	File       *FileHandler
	AIInterface *AIInterfaceHandler
	LLMConfig  *LLMConfigHandler
	AICall     *AICallHandler
	CallLog    *CallLogHandler
	User       *UserHandler
}

// New 创建 Handler
func New(cfg *model.Config, st store.MetaStore) *Handler {
	auth := NewAuthHandler(cfg, st)
	return &Handler{
		Config:      NewConfigHandler(cfg, st),
		Auth:        auth,
		Page:        NewPageHandler(cfg),
		File:        NewFileHandler(cfg, st),
		AIInterface: NewAIInterfaceHandler(st),
		LLMConfig:   NewLLMConfigHandler(st),
		AICall:      NewAICallHandler(st),
		CallLog:     NewCallLogHandler(st),
		User:        NewUserHandler(st, auth),
	}
}