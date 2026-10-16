# 电子公文智能流转系统 - AI 网关实现方案

## 项目名称

`doc-ai-gateway` — 文档 AI 智能网关

## 项目路径

`/home/rd/workspace/doc-ai-gateway`

---

## 1. 架构概览

```
┌──────────────────────────────────────────────────────────────────┐
│                         OA 系统                                   │
│  ① POST /api/files/upload  (multipart, 返回 file_id)             │
│  ② POST /api/ai/call       (file_id + 接口名 + 参数)              │
│  ③ GET  /api/files/status/{file_id} (查询预处理状态)              │
└──────────────────────────┬───────────────────────────────────────┘
                           │ HTTP
┌──────────────────────────▼───────────────────────────────────────┐
│                    doc-ai-gateway                                  │
│                                                                    │
│  ┌─────────────────┐  ┌──────────────────────┐                    │
│  │ 文件预处理服务     │  │ 可配置 LLM 网关       │                   │
│  │ ├─ DOCX 提取     │  │ ├─ 接口管理 CRUD      │                   │
│  │ ├─ 文字PDF 提取  │  │ ├─ 提示词模板渲染      │                   │
│  │ ├─ 扫描PDF OCR   │  │ ├─ 占位符替换         │                   │
│  │ └─ 结果缓存      │  │ ├─ LLM API 调用       │                   │
│  └─────────────────┘  │ └─ JSON Schema 校验    │                   │
│                        └──────────────────────┘                    │
│  ┌──────────────────────────────────────────┐                     │
│  │ Admin GUI (内嵌 Web)                      │                     │
│  │ ├─ 登录/用户管理                          │                     │
│  │ ├─ AI 接口配置 (CRUD)                     │                     │
│  │ ├─ LLM 模型配置                           │                     │
│  │ ├─ 文件列表/预处理状态                     │                     │
│  │ └─ 调用日志查询                           │                     │
│  └──────────────────────────────────────────┘                     │
└──────────────────────────────────────────────────────────────────┘
```

---

## 2. 技术栈

| 层 | 技术 | 来源 |
|----|------|------|
| 语言 | Go 1.24 | 与 pdf2docx/kb-chat-flow 一致 |
| Web 框架 | Gin | 与 kb-chat-flow 一致 |
| 数据库 | SQLite (modernc.org/sqlite) | 与 kb-chat-flow 一致，零依赖 |
| 前端 | 服务端渲染 HTML + 内嵌 JS + CSS | 与 kb-chat-flow admin 面板一致 |
| 嵌入 | embed.FS 打包 Web 资源 | 与 kb-chat-flow 一致 |
| PDF 提取 | github.com/ledongthuc/pdf | 与 kb-chat-flow 一致 |
| DOCX 提取 | archive/zip + encoding/xml | 与 kb-chat-flow 一致 |
| OCR | 可选集成 CGO + ONNX Runtime | 参考 pdf2docx |
| Docker | 多阶段构建 | 参考 pdf2docx |

---

## 3. 目录结构

```
doc-ai-gateway/
├── main.go                       # 入口
├── go.mod
├── go.sum
├── cfg.yml.template              # 配置文件模板
├── cfg.yml                       # 配置文件 (gitignore)
├── cfg.db.template               # SQLite 数据库模板
├── README.md
│
├── internal/
│   ├── config/
│   │   └── config.go             # 配置加载 (YAML + SQLite runtime)
│   ├── model/
│   │   └── types.go              # 所有数据结构定义
│   ├── store/
│   │   ├── interface.go          # 存储接口
│   │   └── sqlite.go             # SQLite 实现
│   ├── handler/
│   │   ├── handler.go            # Handler 聚合
│   │   ├── auth.go               # 登录/认证
│   │   ├── page.go               # 页面渲染
│   │   ├── middleware.go          # 中间件 (Auth, Logging)
│   │   ├── file.go               # 文件上传/预处理
│   │   ├── ai_interface.go       # AI 接口 CRUD
│   │   ├── llm_config.go         # LLM 模型配置 CRUD
│   │   ├── ai_call.go            # 通用 AI 调用 (OA 侧)
│   │   ├── call_log.go           # 调用日志
│   │   └── user.go               # 用户管理
│   ├── preprocess/
│   │   ├── preprocess.go         # 预处理调度器
│   │   ├── docx.go               # DOCX 文本提取
│   │   ├── pdf.go                # PDF 文本提取
│   │   └── ocr.go                # OCR (可选/可替换)
│   ├── llm/
│   │   └── client.go             # LLM API 客户端 (OpenAI 兼容)
│   └── logger/
│       └── logger.go             # 日志
│
├── web/
│   ├── templates/
│   │   ├── login.html            # 登录页
│   │   ├── register.html         # 注册页
│   │   └── admin.html            # 管理后台 (AI接口+模型配置等)
│   └── static/
│       ├── css/
│       │   └── style.css
│       ├── js/
│       │   └── app.js
│       └── lib/
│           ├── font-awesome.all.min.css
│           └── marked.min.js
│
├── scripts/
│   ├── Dockerfile                # 开发/编译镜像
│   ├── Dockerfile.server         # 生产镜像
│   ├── build.sh                  # Linux 编译脚本
│   ├── build.bat                 # Windows 编译脚本
│   └── dev.sh                    # Docker 开发环境入口
│
├── dist/                         # 编译产物
└── testdata/                     # 测试用文件
```

---

## 4. 数据模型

### 4.1 SQLite 表结构

```sql
-- 用户表
CREATE TABLE users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    role INTEGER DEFAULT 0,       -- 0=普通 1=管理员
    note TEXT DEFAULT '',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 系统配置 (KV)
CREATE TABLE sys_config (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    description TEXT DEFAULT ''
);

-- LLM 模型配置
CREATE TABLE llm_configs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,            -- 配置名称，如 "DeepSeek V3"
    api_uri TEXT NOT NULL,         -- API 地址
    api_key TEXT NOT NULL,         -- API Key
    model_name TEXT NOT NULL,      -- 模型名称
    temperature REAL DEFAULT 0.7,
    max_tokens INTEGER DEFAULT 4096,
    is_default INTEGER DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- AI 接口配置 (管理员可配置的 LLM 调用接口)
CREATE TABLE ai_interfaces (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,            -- 接口名称，如 "收文类型判断"
    path TEXT UNIQUE NOT NULL,     -- 接口路径，如 "receive-type"
    description TEXT DEFAULT '',
    prompt_template TEXT NOT NULL, -- 提示词模板，含 {{占位符}}
    input_params TEXT DEFAULT '[]',-- JSON: [{"name":"file_text","source":"file.text","required":true},...]
    output_schema TEXT DEFAULT '{}', -- JSON Schema 约束输出格式
    llm_config_id INTEGER,         -- 使用的 LLM 模型配置
    temperature REAL,              -- 可为 NULL (用默认值)
    max_tokens INTEGER,
    enabled INTEGER DEFAULT 1,
    version INTEGER DEFAULT 1,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (llm_config_id) REFERENCES llm_configs(id)
);

-- 上传文件记录
CREATE TABLE files (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    file_id TEXT UNIQUE NOT NULL,  -- 业务标识 "F-20261027-001"
    original_name TEXT NOT NULL,
    file_type TEXT DEFAULT '',     -- docx/pdf/image/ofd
    file_path TEXT NOT NULL,       -- 本地存储路径
    text_content TEXT DEFAULT '',  -- 预处理提取的文本
    text_length INTEGER DEFAULT 0,
    ocr_confidence REAL,
    status TEXT DEFAULT 'processing', -- processing/ready/failed
    error_message TEXT DEFAULT '',
    biz_id TEXT DEFAULT '',        -- OA 流程实例 ID
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- API 调用日志
CREATE TABLE call_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    file_id TEXT NOT NULL,
    interface_id INTEGER,
    interface_path TEXT NOT NULL,
    input_params TEXT DEFAULT '{}',
    output_result TEXT DEFAULT '{}',
    status TEXT DEFAULT 'success', -- success/error
    error_message TEXT DEFAULT '',
    elapsed_ms INTEGER DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (interface_id) REFERENCES ai_interfaces(id)
);

-- API Token (供 OA 系统调用)
CREATE TABLE api_tokens (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    token TEXT UNIQUE NOT NULL,
    user_id INTEGER,
    description TEXT DEFAULT '',
    enabled INTEGER DEFAULT 1,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id)
);
```

### 4.2 Go 数据结构

```go
// AIInterface 管理员配置的 AI 接口
type AIInterface struct {
    ID              int64     `json:"id"`
    Name            string    `json:"name"`
    Path            string    `json:"path"`
    Description     string    `json:"description"`
    PromptTemplate  string    `json:"prompt_template"`
    InputParams     string    `json:"input_params"`      // JSON array
    OutputSchema    string    `json:"output_schema"`     // JSON schema
    LLMConfigID     *int64    `json:"llm_config_id"`
    Temperature     *float64  `json:"temperature"`
    MaxTokens       *int      `json:"max_tokens"`
    Enabled         bool      `json:"enabled"`
    Version         int       `json:"version"`
    CreatedAt       string    `json:"created_at"`
    UpdatedAt       string    `json:"updated_at"`
}

// InputParam 占位符定义
type InputParam struct {
    Name     string `json:"name"`               // 占位符名称
    Source   string `json:"source"`             // "file.text" | "file.meta" | "params.xxx"
    Required bool   `json:"required"`
    Default  string `json:"default,omitempty"`
}

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
}

// FileRecord 上传文件记录
type FileRecord struct {
    ID            int64   `json:"id"`
    FileID        string  `json:"file_id"`
    OriginalName  string  `json:"original_name"`
    FileType      string  `json:"file_type"`
    FilePath      string  `json:"-"`
    TextContent   string  `json:"-"`
    TextLength    int     `json:"text_length"`
    OCRConfidence float64 `json:"ocr_confidence,omitempty"`
    Status        string  `json:"status"`
    ErrorMessage  string  `json:"error_message,omitempty"`
    BizID         string  `json:"biz_id"`
    CreatedAt     string  `json:"created_at"`
}
```

---

## 5. API 设计

### 5.1 对外接口 (OA 系统调用)

#### `POST /api/files/upload` — 文件上传
```
Content-Type: multipart/form-data

Fields:
  file:      (binary, required)
  biz_id:    "OA-FLOW-20261027-001"
  file_name: "关于xxx的通知.pdf"

Response 200:
{
  "file_id": "F-20261027001",
  "status": "processing",
  "file_type": "pdf",
  "message": "文件已接收，预处理中"
}
```

内部流程：
1. 接收文件，生成 file_id (F-YYYYMMDD序号)
2. 保存到 `data/files/{file_id}/` 目录
3. 异步启动预处理：
   - .docx → 解压 XML 提取文本
   - 文字 PDF → ledongthuc/pdf 提取文本
   - 扫描 PDF → OCR (可选)
4. 预处理完成后更新 status → "ready"，填充 text_content

#### `POST /api/ai/call` — 通用 AI 调用
```json
{
  "file_id": "F-20261027001",
  "interface": "receive-type",
  "params": {
    "node_name": "收文登记",
    "form_type": ""
  }
}

Response 200:
{
  "file_id": "F-20261027001",
  "interface": "receive-type",
  "status": "success",
  "result": {
    "type": "公司收文",
    "confidence": 0.96
  },
  "elapsed_ms": 1200
}
```

内部流程：
1. 根据 `interface` 路径查找 `ai_interfaces` 表
2. 检查 interface 是否 enabled
3. 从 `files` 表获取预处理文本
4. 渲染提示词模板（替换 `{{占位符}}`）：
   - `{{file.text}}` → 文件预处理文本
   - `{{file.name}}` → 原始文件名
   - `{{params.xxx}}` → OA 传入的参数
5. 调用 LLM API（使用关联的 llm_config）
6. 校验输出 JSON Schema
7. 记录调用日志
8. 返回结果

#### `GET /api/files/status/{file_id}` — 查询文件状态
```json
{
  "file_id": "F-20261027001",
  "status": "ready",
  "file_type": "pdf",
  "text_length": 3240,
  "ocr_confidence": null,
  "created_at": "2026-10-27T10:30:00Z"
}
```

### 5.2 管理后台 API (Cookie 认证)

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/login` | 登录页 |
| POST | `/api/login` | 登录 |
| POST | `/api/logout` | 登出 |
| GET | `/admin` | 管理后台主页 |
| GET | `/api/v1/interfaces` | 列出 AI 接口 |
| POST | `/api/v1/interfaces` | 创建 AI 接口 |
| PUT | `/api/v1/interfaces/:id` | 更新 AI 接口 |
| DELETE | `/api/v1/interfaces/:id` | 删除 AI 接口 |
| POST | `/api/v1/interfaces/:id/test` | 测试 AI 接口 |
| GET | `/api/v1/llm-configs` | 列出 LLM 配置 |
| POST | `/api/v1/llm-configs` | 创建 LLM 配置 |
| PUT | `/api/v1/llm-configs/:id` | 更新 LLM 配置 |
| DELETE | `/api/v1/llm-configs/:id` | 删除 LLM 配置 |
| POST | `/api/v1/llm-configs/test` | 测试 LLM 连接 |
| GET | `/api/v1/files` | 文件列表 |
| DELETE | `/api/v1/files/:id` | 删除文件及缓存 |
| GET | `/api/v1/call-logs` | 调用日志列表 |
| GET | `/api/v1/users` | 用户列表 |
| POST | `/api/v1/users` | 创建用户 |
| DELETE | `/api/v1/users/:name` | 删除用户 |
| PUT | `/api/v1/user/password` | 修改密码 |
| GET | `/api/v1/config` | 获取系统配置 |
| PUT | `/api/v1/config` | 更新系统配置 |

---

## 6. 管理后台 GUI 设计

### 6.1 布局

参考 `kb-chat-flow/g/web/templates/config.html` 的模式：

```
┌─────────────────────────────────────────────────┐
│  DocAI Gateway - 系统管理          [退出]       │
├──────────┬──────────────────────────────────────┤
│ 左侧导航  │ 右侧内容区                            │
│          │                                      │
│ 📋 接口管理│  ┌─ AI 接口列表 ──────────────────┐  │
│         │  │ 名称        │ 路径     │ 状态 │操作│  │
│ 🤖 LLM  │  │ 收文类型判断 │receive..│ 启用 │编辑│  │
│   模型配置│  │ 拟办意见生成 │draft-.. │ 启用 │编辑│  │
│          │  │ 格式审核    │format.. │ 禁用 │编辑│  │
│ 📁 文件管理│  └────────────────────────────────┘  │
│          │                                      │
│ 📊 调用日志│  [+ 新建接口]                         │
│          │                                      │
│ ⚙ 系统设置│                                      │
│          │                                      │
│ 👤 用户管理│                                      │
└──────────┴──────────────────────────────────────┘
```

### 6.2 核心页面：AI 接口编辑弹窗

```
┌── 编辑 AI 接口 ──────────────────────────────────┐
│                                                    │
│  接口名称:  [收文类型判断________]                   │
│  接口路径:  [receive-type_______]                   │
│  描述:      [判断文件属于公司收文还是部门收文___]      │
│                                                    │
│  关联模型:  [DeepSeek V3 ▼]                        │
│  Temperature: [0.3]  MaxTokens: [1024]             │
│                                                    │
│  提示词模板:                                        │
│  ┌──────────────────────────────────────────────┐  │
│  │ 请根据以下文件内容判断收文类型：                │  │
│  │                                              │  │
│  │ 文件内容：{{file.text}}                       │  │
│  │                                              │  │
│  │ 当前环节：{{params.node_name}}                │  │
│  │ 表单类型：{{params.form_type}}                │  │
│  │                                              │  │
│  │ 可能的类型：公司收文、部门收文                  │  │
│  │                                              │  │
│  │ 请返回 JSON: {"type":"...", "confidence":0.0} │  │
│  └──────────────────────────────────────────────┘  │
│                                                    │
│  输入参数:                                [+ 添加]  │
│  ┌──────┬──────────────┬──────┬──────────────────┐ │
│  │ 名称   │ 来源          │ 必填  │ 默认值            │ │
│  ├──────┼──────────────┼──────┼──────────────────┤ │
│  │ node_name│ params.node_.│ ✓    │                  │ │
│  │ form_type│ params.form_.│      │ ""               │ │
│  └──────┴──────────────┴──────┴──  ────────────────┘ │
│                                                    │
│  输出 JSON Schema:                                  │
│  ┌──────────────────────────────────────────────┐  │
│  │ {                                            │  │
│  │   "type": "object",                          │  │
│  │   "properties": {                            │  │
│  │     "type": {"enum": ["公司收文","部门收文"]}, │  │
│  │     "confidence": {"type": "number"}          │  │
│  │   }                                          │  │
│  │ }                                            │  │
│  └──────────────────────────────────────────────┘  │
│                                                    │
│  ☑ 启用此接口                                       │
│                                                    │
│  [保存]  [测试]  [取消]                              │
└────────────────────────────────────────────────────┘
```

### 6.3 内置占位符 (自动可用，无需管理员定义)

| 占位符 | 说明 | 来源 |
|--------|------|------|
| `{{file.text}}` | 文件预处理后的纯文本全文 | 预处理服务 |
| `{{file.name}}` | 原始文件名 | 上传时记录 |
| `{{file.type}}` | 文件类型 (docx/pdf/image) | 预处理检测 |
| `{{file.page_count}}` | 页数 (PDF) | 预处理检测 |
| `{{params.xxx}}` | OA 在 /api/ai/call 传入的自定义参数 | OA 系统 |

---

## 7. 核心流程

### 7.1 文件预处理流程

```
OA 上传文件 (multipart/form-data)
    │
    ▼
生成 file_id → 保存到 data/files/{file_id}/
    │
    ▼
判断文件类型 (MIME + 扩展名)
    │
    ├─ application/vnd.openxmlformats... (.docx)
    │    → docx.go: 解压 ZIP → 解析 word/document.xml → 提取 <w:t>
    │
    ├─ application/pdf (.pdf)
    │    → pdf.go: ledongthuc/pdf 尝试提取文字
    │    → 有文字: status=ready
    │    → 无文字: 标记为扫描件 → 可选OCR
    │
    ├─ image/* (.jpg/.png/.bmp)
    │    → 直接走 OCR
    │
    └─ application/vnd.openxmlformats... (.xlsx)
         → xlsx.go: 解压 → sharedStrings + sheet → 提取文本
    │
    ▼
更新 files 表: text_content, text_length, status, file_type
```

**关于 OCR：**
- 第一版可以不带 OCR，扫描件返回 `status=failed, error="扫描件需OCR，当前未配置"`
- OCR 模块可以后续从 pdf2docx 移植 CGO + ONNX Runtime
- 也可以对接外部 OCR API

### 7.2 AI 调用流程

```
OA 调用 POST /api/ai/call { file_id, interface: "xxx", params: {...} }
    │
    ▼
查找 ai_interfaces WHERE path = "xxx" AND enabled = 1
    │ 找不到 → 返回 404
    ▼
查找 files WHERE file_id = ?
    │ status = "processing" → 返回 202 "请稍后重试"
    │ status = "failed" → 返回 422 "文件预处理失败: ..."
    │ status = "ready" → 继续
    ▼
构建 prompt:
  1. 解析 input_params → 获取占位符列表
  2. 替换 {{file.text}} → files.text_content
  3. 替换 {{file.name}} → files.original_name
  4. 替换 {{file.type}} → files.file_type
  5. 替换 {{params.xxx}} → 从请求 params 提取
  6. 未提供的可选参数 → 用 default 值
  7. 缺少必填参数 → 返回 400
    ▼
获取 LLM 配置:
  1. 如果 ai_interface.llm_config_id 有值 → 用指定模型
  2. 否则用 is_default=1 的 LLM 配置
  3. 没有默认模型 → 返回 500
    ▼
调用 LLM:
  system: "你是一个公文处理助手，请严格按照要求的 JSON 格式返回结果。"
  user:   <渲染后的 prompt_template>
  temperature/max_tokens: interface 配置覆盖默认
    ▼
解析 LLM 返回:
  1. 尝试解析 JSON
  2. 如果有 output_schema → JSON Schema 校验
  3. 校验失败 → 重试一次
  4. 仍然失败 → 返回原始文本 + warning
    ▼
记录 call_logs → 返回结构化结果
```

---

## 8. 编译与部署

### 8.1 Go Module

使用无 CGO 编译（第一版不集成 OCR），纯 Go 实现：
- `github.com/gin-gonic/gin` — Web 框架
- `github.com/ledongthuc/pdf` — PDF 文本提取
- `modernc.org/sqlite` — 纯 Go SQLite
- `golang.org/x/crypto` — 密码哈希

### 8.2 编译脚本

#### `scripts/build.sh` (Linux)
```bash
#!/usr/bin/env bash
# 1. 进入 Docker 编译容器 (golang:1.24)
# 2. go build -ldflags="-s -w" -o dist/doc-ai-gateway .
# 3. 构建 Docker 生产镜像
```

#### `scripts/build.bat` (Windows)
```bat
:: 1. 检测本地 Go 安装
:: 2. go build -ldflags="-s -w" -o dist/doc-ai-gateway.exe .
:: 3. 复制 cfg.db.template → dist/cfg.db
:: 4. 打包 dist/doc-ai-gateway-windows-amd64.zip
```

### 8.3 Docker 部署

```dockerfile
# 编译阶段
FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -ldflags="-s -w" -o doc-ai-gateway .

# 运行阶段
FROM alpine:3.21
WORKDIR /app
COPY --from=builder /app/doc-ai-gateway .
COPY cfg.db.template /app/cfg.db
COPY data/ /app/data/
EXPOSE 8080
ENTRYPOINT ["./doc-ai-gateway"]
```

### 8.4 Windows 独立运行

```
doc-ai-gateway-windows-amd64/
├── doc-ai-gateway.exe    # 编译好的可执行文件
├── cfg.db                # 预置数据库 (含 admin/admin 账号)
├── cfg.yml               # 配置文件
├── web/                  # 静态资源 (已 embed，不需要)
└── data/                 # 文件存储目录
    └── files/
```

Windows 用户双击 `doc-ai-gateway.exe`，访问 `http://localhost:8080` 即可使用。
自带模拟 OA 提交的界面，可直接上传文件、选择接口、查看结果。

---

## 9. 实现顺序

| 阶段 | 内容 | 预计文件 |
|------|------|---------|
| **Phase 1: 骨架** | go.mod, main.go, config, logger, store/sqlite, model/types | ~8 files |
| **Phase 2: 认证** | handler/auth, handler/page, handler/middleware, login.html | ~6 files |
| **Phase 3: 文件管理** | handler/file, preprocess/*, POST /api/files/upload | ~5 files |
| **Phase 4: LLM 配置** | handler/llm_config, LLM 配置 CRUD + 测试连接 | ~2 files |
| **Phase 5: AI 接口管理** | handler/ai_interface, 接口 CRUD + 测试 | ~2 files |
| **Phase 6: AI 调用** | handler/ai_call, POST /api/ai/call, 占位符渲染 | ~2 files |
| **Phase 7: 调用日志** | handler/call_log, 日志列表查询 | ~2 files |
| **Phase 8: 管理后台** | admin.html, 用户管理, 系统设置 | ~3 files |
| **Phase 9: 编译脚本** | scripts/build.sh, scripts/build.bat, Dockerfile | ~4 files |
| **Phase 10: Windows 自测** | 模拟 OA 提交界面, 一键体验 | ~2 files |

总计约 **35-40 个源文件**。

---

## 10. 与参考项目的关系

| 能力 | 来源 |
|------|------|
| 项目骨架 (main.go, config, store) | kb-chat-flow/g |
| Web 框架路由 (gin, embed, template) | kb-chat-flow/g |
| 登录页面样式 | kb-chat-flow/g/web/templates/login.html |
| 管理后台布局 (左侧导航 + 右侧内容) | kb-chat-flow/g/web/templates/config.html |
| LLM 客户端 (OpenAI 兼容) | kb-chat-flow/g/internal/llm/client.go |
| PDF/DOCX 文本提取 | kb-chat-flow/g/internal/kb/extract.go |
| Docker 编译环境 | pdf2docx/scripts/Dockerfile |
| 编译脚本模式 | pdf2docx/scripts/build_server.sh |
| Windows 编译脚本 | pdf2docx/scripts/build.bat |
| Docker 生产镜像 | pdf2docx/scripts/Dockerfile.server |