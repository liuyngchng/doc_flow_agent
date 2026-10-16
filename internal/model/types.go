package model

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
	Server ServerConfig `yaml:"server"`
	Sys    SysConfig    `yaml:"sys"`
}

// ServerConfig 服务器配置
type ServerConfig struct {
	Port  int  `yaml:"port"`
	Debug bool `yaml:"debug"`
}

// SysConfig 系统配置
type SysConfig struct {
	Name     string `yaml:"name"`
	BasePath string `yaml:"base_path"`
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
// AI 接口配置
// ============================================================

// AIInterface 管理员配置的 AI 接口
type AIInterface struct {
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
	ID            int64   `json:"id"`
	FileID        string  `json:"file_id"`
	OriginalName  string  `json:"original_name"`
	FileType      string  `json:"file_type"`
	FilePath      string  `json:"file_path"`
	TextContent   string  `json:"-"`
	TextLength    int     `json:"text_length"`
	OCRConfidence float64 `json:"ocr_confidence,omitempty"`
	Status        string  `json:"status"`
	ErrorMessage  string  `json:"error_message,omitempty"`
	BizID         string  `json:"biz_id"`
	CreatedAt     string  `json:"created_at"`
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