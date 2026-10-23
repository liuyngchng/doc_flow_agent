package model

// ============================================================
// 业务错误码（与电子公文流转接口文档 V1.0 对齐）
// ============================================================
const (
	ErrCodeSuccess        = 0
	ErrCodeParamError     = 1001 // 参数错误
	ErrCodeFileNotFound   = 1002 // 文件不存在
	ErrCodeFileUnreadable = 1003 // 文件不可读
	ErrCodeAgentFail      = 1004 // 智能体分析失败（需人工）
	ErrCodeTimeout        = 2001 // 接口超时
	ErrCodeQueueFull      = 2002 // 任务队列已满
	ErrCodeInternal       = 5000 // 内部错误
)

// APIResponse 通用 API 响应（对齐电子公文流转接口文档格式）
type APIResponse struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data"`
}

// NewSuccessResponse 成功响应
func NewSuccessResponse(data interface{}) APIResponse {
	return APIResponse{Code: ErrCodeSuccess, Message: "success", Data: data}
}

// NewErrorResponse 错误响应
func NewErrorResponse(code int, message string) APIResponse {
	return APIResponse{Code: code, Message: message, Data: nil}
}

// ============================================================
// 角色常量
// ============================================================
const (
	RoleNormal = 0
	RoleAdmin  = 1
)

// ============================================================
// 文件预处理
// ============================================================
const (
	FileStatusProcessing = "processing"
	FileStatusReady      = "ready"
	FileStatusFailed     = "failed"
)

// User 用户
type User struct {
	ID            int64  `json:"id"`
	UserName      string `json:"user_name"`
	PasswordHash  string `json:"-"`
	Role          int    `json:"role"`
	Note          string `json:"note"`
	MustChangePwd bool   `json:"must_change_pwd"`
	CreatedAt     string `json:"created_at"`
}

// Config 运行配置
type Config struct {
	Server    ServerConfig    `yaml:"server"`
	Sys       SysConfig       `yaml:"sys"`
	DocParser DocParserConfig `yaml:"doc_parser"`
	Dict      map[string]string `yaml:"dict"` // 数据字典：请求 JSON key → 中文含义
}

// ServerConfig 服务器配置
type ServerConfig struct {
	Port     int    `yaml:"port"`
	Debug    bool   `yaml:"debug"`
	CertFile string `yaml:"cert_file"` // HTTPS 证书文件路径，为空则使用 HTTP
	KeyFile  string `yaml:"key_file"`  // HTTPS 私钥文件路径，为空则使用 HTTP
}

// SysConfig 系统配置
type SysConfig struct {
	Name                  string `yaml:"name"`
	BasePath              string `yaml:"base_path"`
	CallbackToken         string `yaml:"callback_url_token"`          // 回调 Bearer 认证 token
	MaxConcurrentTasks    int    `yaml:"max_concurrent_tasks"`        // Worker 数，默认 5
	MaxPendingTasks       int    `yaml:"max_pending_tasks"`           // 任务总数上限，默认 10
	CallbackRetries       int    `yaml:"callback_retries"`            // 回调重试次数，默认 3
	CallbackRetryInterval int    `yaml:"callback_retry_interval_sec"` // 重试基础间隔（秒），默认 2
	RetentionDays         int    `yaml:"retention_days"`              // 数据保留天数，默认 30
}

// DocParserConfig 文档解析服务配置
type DocParserConfig struct {
	URL   string `yaml:"url"`   // 文档解析接口地址，如 http://parser:9090/parse
	Token string `yaml:"token"` // 调用解析接口时携带的认证 token（可选）
}

// ============================================================
// LLM 模型配置
// ============================================================

// LLMConfig LLM 模型配置
type LLMConfig struct {
	ID          int64   `json:"id"`
	Name        string  `json:"name"`
	APIURI      string  `json:"api_uri"`
	APIKey      string  `json:"api_key"`
	ModelName   string  `json:"model_name"`
	Temperature float64 `json:"temperature"`
	MaxTokens   int     `json:"max_tokens"`
	IsDefault   bool    `json:"is_default"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

// ============================================================
// 接口配置
// ============================================================

// InterfaceConfig 管理员配置的接口
type InterfaceConfig struct {
	ID             int64    `json:"id"`
	Name           string   `json:"name"`
	Path           string   `json:"path"`
	Description    string   `json:"description"`
	PromptTemplate string   `json:"prompt_template"`
	InputParams    []InputParam `json:"input_params"`
	OutputSchema   string   `json:"output_schema"`
	LLMConfigID    *int64   `json:"llm_config_id"`
	Temperature    *float64 `json:"temperature"`
	MaxTokens      *int     `json:"max_tokens"`
	Enabled        bool     `json:"enabled"`
	Version        int      `json:"version"`
	CreatedAt      string   `json:"created_at"`
	UpdatedAt      string   `json:"updated_at"`
}

// InputParam 占位符定义
type InputParam struct {
	Name     string `json:"name"`
	Source   string `json:"source"` // "file.text" | "file.name" | "file.type" | "params.xxx"
	Required bool   `json:"required"`
	Default  string `json:"default,omitempty"`
}

// ============================================================
// 文件记录
// ============================================================

// FileRecord 上传文件记录
type FileRecord struct {
	ID            int64    `json:"id"`
	FileID        string   `json:"file_id"`
	OriginalName  string   `json:"original_name"`
	FileType      string   `json:"file_type"`
	FilePath      string   `json:"file_path"`
	TextContent   string   `json:"-"`
	TextLength    int      `json:"text_length"`
	OCRConfidence float64  `json:"ocr_confidence,omitempty"`
	HasSeal       bool     `json:"has_seal,omitempty"`
	SealText      []string `json:"seal_text,omitempty"`
	Status        string   `json:"status"`
	ErrorMessage  string   `json:"error_message,omitempty"`
	BizID         string   `json:"biz_id"`
	CreatedAt     string   `json:"created_at"`
}

// ParsedDocument 文档解析结果（本地解析 / 远程解析服务统一返回）
type ParsedDocument struct {
	Text          string   `json:"text"`           // 提取的纯文本内容
	OCRConfidence float64  `json:"ocr_confidence"` // OCR 置信度
	HasSeal       bool     `json:"has_seal"`       // 是否检测到印章
	SealText      []string `json:"seal_text"`      // 印章文字列表
}

// ============================================================
// 异步任务
// ============================================================

const (
	TaskStatusPending      = "pending"
	TaskStatusProcessing   = "processing"
	TaskStatusSuccess      = "success"
	TaskStatusFailed       = "failed"
	TaskStatusCompleted    = "completed"
	TaskStatusCallbackFail = "callback_fail"
)

// AsyncTask 异步任务记录
type AsyncTask struct {
	ID            int64  `json:"id"`
	TaskID        string `json:"task_id"`
	LinkID        string `json:"link_id"` // 对方系统的 linkid（可能不唯一）
	InterfacePath string `json:"interface_path"`
	RequestBody   string `json:"request_body"`
	CallbackURL   string `json:"callback_url"`
	Status        string `json:"status"`
	OutputResult  string `json:"output_result"`
	ErrorMessage  string `json:"error_message"`
	ElapsedMs     int64  `json:"elapsed_ms"`
	RetryCount    int    `json:"retry_count"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

// ============================================================
// 调用日志
// ============================================================

// CallLog API 调用日志
type CallLog struct {
	ID            int64  `json:"id"`
	FileID        string `json:"file_id"`
	InterfaceID   int64  `json:"interface_id"`
	InterfacePath string `json:"interface_path"`
	InputParams   string `json:"input_params"`
	OutputResult  string `json:"output_result"`
	Status        string `json:"status"`
	ErrorMessage  string `json:"error_message,omitempty"`
	ElapsedMs     int64  `json:"elapsed_ms"`
	CreatedAt     string `json:"created_at"`
}

// ============================================================
// API Token
// ============================================================

// APIToken API 访问令牌
type APIToken struct {
	ID          int64  `json:"id"`
	Token       string `json:"token"`
	UserID      int64  `json:"user_id"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
	CreatedAt   string `json:"created_at"`
}

// ============================================================
// LLM 调用相关
// ============================================================

// ChatCompletionMsg 消息
type ChatCompletionMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatCompletionRequest OpenAI 兼容请求
type ChatCompletionRequest struct {
	Model       string              `json:"model"`
	Messages    []ChatCompletionMsg `json:"messages"`
	Stream      bool                `json:"stream"`
	Temperature *float64            `json:"temperature,omitempty"`
	TopP        *float64            `json:"top_p,omitempty"`
	MaxTokens   *int                `json:"max_tokens,omitempty"`
}

// ChatCompletionChunk 流式响应片段
type ChatCompletionChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
}