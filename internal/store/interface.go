package store

import "doc-ai-gateway/internal/model"

// MetaStore 元数据存储接口
type MetaStore interface {
	Close() error

	// --- 用户 ---
	GetUser(userName string) (*model.User, error)
	GetUserByID(id int64) (*model.User, error)
	ListUsers() ([]model.User, error)
	CreateUser(user *model.User) error
	DeleteUser(userName string) error
	UpdatePassword(userName string, hash string) error
	ClearMustChangePwd(userName string) error

	// --- 系统配置 (KV) ---
	GetConfig(key string) (string, error)
	SetConfig(key, value, description string) error

	// --- LLM 模型配置 ---
	ListLLMConfigs() ([]model.LLMConfig, error)
	GetLLMConfig(id int64) (*model.LLMConfig, error)
	GetDefaultLLMConfig() (*model.LLMConfig, error)
	CreateLLMConfig(cfg *model.LLMConfig) error
	UpdateLLMConfig(cfg *model.LLMConfig) error
	DeleteLLMConfig(id int64) error
	SetDefaultLLMConfig(id int64) error

	// --- AI 接口配置 ---
	ListAIInterfaces() ([]model.AIInterface, error)
	GetAIInterfaceByID(id int64) (*model.AIInterface, error)
	GetAIInterfaceByPath(path string) (*model.AIInterface, error)
	CreateAIInterface(iface *model.AIInterface) error
	UpdateAIInterface(iface *model.AIInterface) error
	DeleteAIInterface(id int64) error
	IncrementInterfaceVersion(id int64) error

	// --- 文件记录 ---
	CreateFileRecord(record *model.FileRecord) error
	GetFileRecord(fileID string) (*model.FileRecord, error)
	ListFileRecords(limit, offset int) ([]model.FileRecord, int, error)
	UpdateFileStatus(fileID, status, errMsg string) error
	UpdateFileText(fileID, text string, length int, ocrConf float64) error
	DeleteFileRecord(fileID string) error

	// --- 调用日志 ---
	CreateCallLog(log *model.CallLog) error
	ListCallLogs(fileID string, limit, offset int) ([]model.CallLog, int, error)
}