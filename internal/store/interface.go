package store

import "doc_flow_agent/internal/model"

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

	// --- 接口配置 ---
	ListInterfaces() ([]model.InterfaceConfig, error)
	GetInterfaceByID(id int64) (*model.InterfaceConfig, error)
	GetInterfaceByPath(path string) (*model.InterfaceConfig, error)
	CreateInterface(iface *model.InterfaceConfig) error
	UpdateInterface(iface *model.InterfaceConfig) error
	DeleteInterface(id int64) error
	IncrementInterfaceVersion(id int64) error

	// --- 文件记录 ---
	CreateFileRecord(record *model.FileRecord) error
	GetFileRecord(fileID string) (*model.FileRecord, error)
	ListFileRecords(limit, offset int) ([]model.FileRecord, int, error)
	UpdateFileStatus(fileID, status, errMsg string) error
	UpdateFileParseResult(fileID string, pd *model.ParsedDocument) error
	DeleteFileRecord(fileID string) error

	// --- 调用日志 ---
	CreateCallLog(log *model.CallLog) error
	ListCallLogs(fileID string, limit, offset int) ([]model.CallLog, int, error)
}