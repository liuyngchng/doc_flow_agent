package handler

import (
	"doc_flow_agent/internal/model"
	"doc_flow_agent/internal/store"
)

// Handler 聚合所有处理器
type Handler struct {
	Config    *ConfigHandler
	Auth      *AuthHandler
	Page      *PageHandler
	File      *FileHandler
	Interface *InterfaceHandler
	LLMConfig *LLMConfigHandler
	Call      *CallHandler
	CallLog   *CallLogHandler
	User      *UserHandler
	Proofread *ProofreadHandler
	AsyncTask *AsyncTaskHandler
	Worker    *AsyncWorker
}

// New 创建 Handler
func New(cfg *model.Config, st store.MetaStore) *Handler {
	auth := NewAuthHandler(cfg, st)
	call := NewCallHandler(st, cfg)
	worker := NewAsyncWorker(st, cfg)
	call.SetWorker(worker)

	return &Handler{
		Config:    NewConfigHandler(cfg, st),
		Auth:      auth,
		Page:      NewPageHandler(cfg),
		File:      NewFileHandler(cfg, st),
		Interface: NewInterfaceHandler(st),
		LLMConfig: NewLLMConfigHandler(st),
		Call:      call,
		CallLog:   NewCallLogHandler(st),
		User:      NewUserHandler(st, auth),
		Proofread: NewProofreadHandler(st),
		AsyncTask: NewAsyncTaskHandler(st, cfg),
		Worker:    worker,
	}
}