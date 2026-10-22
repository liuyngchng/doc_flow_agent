# doc_flow_agent - 文档 AI 智能网关

面向 电子公文工作流的通用 AI 网关，将文件预处理与可配置的 LLM 调用接口解耦。

## 功能

- **文件预处理**：DOCX / 文字 PDF / XLSX 文本提取（扫描件 OCR 可后续扩展）
- **可配置 LLM 接口**：管理员在 GUI 中配置提示词模板、输入占位符、输出 Schema
- **通用 AI 调用**：电子公文工作流上传文件一次，通过 `file_id` 多次调用不同 AI 接口
- **管理后台**：登录、AI 接口管理、LLM 模型配置、文件管理、调用日志、模拟公文处理

## 架构

```
电子公文工作流 ──HTTP──> doc_flow_agent ──> LLM API
                     │
                     ├── 文件预处理 (docx/pdf/xlsx 文本提取)
                     ├── 可配置 AI 接口 (提示词模板 + 占位符 + Schema)
                     └── Admin GUI (登录 + CRUD + 日志)
```

## 快速开始

### Windows

```bash
scripts/build.bat          # 一键编译
cd dist/doc_flow_agent-windows-amd64
doc_flow_agent.exe         # 双击运行
```

访问 https://localhost:20001/admin ，默认账号 `admin` / `admin`。

> **注意**：首次访问时浏览器会提示证书不安全（自签证书），点击"高级"→"继续访问"即可。
> admin 首次登录必须修改密码，**2 小时内未修改则初始密码作废**，需删库重建。

### Linux / Docker

```bash
scripts/build.sh           # 编译 + 构建 Docker 镜像
docker run -p 20001:20001 -v $(pwd)/data:/app/data doc_flow_agent:latest
```

## 对外 API（电子公文工作流调用）

### 1. 上传文件

```bash
curl -k -X POST https://localhost:20001/api/files/upload \
  -F "file=@test.pdf" \
  -F "biz_id=FLOW-001"
# 返回: {"file_id":"F-20261027001","status":"processing",...}
```

### 2. 查询文件状态

```bash
curl -k -X GET https://localhost:20001/api/files/status/F-20261027001
```

### 3. 调用 AI 接口

```bash
curl -k -X POST https://localhost:20001/api/ai/call \
  -H "Content-Type: application/json" \
  -d '{
    "file_id": "F-20261027001",
    "interface": "receive-type",
    "params": {"node_name": "收文登记"}
  }'
```

## 管理后台 API

| 方法 | 路径 | 说明 |
|------|------|------|
| GET/POST | `/api/inner/interfaces` | AI 接口列表/创建 |
| PUT/DELETE | `/api/inner/interfaces/:id` | 更新/删除接口 |
| POST | `/api/inner/interfaces/:id/test` | 测试接口 |
| GET/POST | `/api/inner/llm-configs` | LLM 配置列表/创建 |
| PUT/DELETE | `/api/inner/llm-configs/:id` | 更新/删除配置 |
| PUT | `/api/inner/llm-configs/:id/default` | 设为默认 |
| GET | `/api/inner/files` | 文件列表 |
| DELETE | `/api/inner/files/:id` | 删除文件 |
| GET | `/api/inner/call-logs` | 调用日志 |

## HTTP 接口工作流程

系统创建的 AI 接口（`POST /api/v1/{接口路径}`）按以下 5 步自动处理每次请求：

### 第一步：下载文件并解析

- 递归扫描请求 JSON，检测所有 `file_url` 字段
- 每个 `file_url` 对应的 WPS/PDF 文档自动下载到临时文件
- 调用文档解析接口（`cfg.yml` 中 `doc_parser.url`）或内置解析器
- 解析接口返回 `{text, ocr_confidence, has_seal, seal_text}`：
  - `text` — 文档文本内容
  - `ocr_confidence` — OCR 置信度（扫描件）
  - `has_seal` — 是否检测到印章
  - `seal_text` — 印章文字列表（如 `["北京XX科技有限公司", "财务专用章"]`）
- 解析结果（文本、印章信息）**写入数据库**（`files` 表），后续可溯源

### 第二步：数据字典翻译 JSON

- 将请求 JSON 中的 key 按 `cfg.yml` 中 `dict`（数据字典）翻译成中文
- 例如 `file_type` → `文件类型`，`secret_level` → `密级`
- 翻译后的 JSON 通过 `{{request.dict}}` 占位符注入提示词，帮助 LLM 准确理解字段含义

### 第三步：组装提示词

- 按接口配置的 `prompt_template` 模板，替换占位符：
  - `{{request.body}}` — 原始请求 JSON
  - `{{request.dict}}` — 翻译成中文 key 后的 JSON
  - `{{file.main_body}}` — 正文文档全文
  - `{{file.attachments}}` — 附件文档全文
  - `{{file.doc_basis}}` — 办文依据文档全文
  - `{{file.seal_text}}` — 所有印章文字汇总
  - `{{file.all_text}}` — 全部文档文本（分章节）
- 每个文件文本块自带文件名前缀和印章文字说明

### 第四步：提交 LLM

- 组装 `system`（系统角色提示）+ `user`（组装好的提示词）
- 调用配置的 LLM（OpenAI 兼容接口），要求返回 JSON

### 第五步：返回结果

- 解析 LLM 输出，包装为统一响应格式 `{code, message, data}`
- 记录调用日志（请求参数、耗时、输出）

---

## 文件来源约定

电子公文工作流可在请求 JSON 中嵌入 `file_url` 字段来引用待解析的 WPS/PDF 文档：

```json
{
  "title": "关于XXXX的通知",
  "doc_type": "通知",
  "main_body": {
    "file_url": "https://wps.example.com/files/xxx.docx",
    "file_type": "main_body",
    "file_name": "通知正文.docx"
  },
  "attachments": [{
    "file_url": "https://wps.example.com/files/yyy.xlsx",
    "file_type": "attachment",
    "file_name": "附件清单.xlsx"
  }]
}
```

| 字段 | 说明 |
|------|------|
| `file_url` | 文档下载链接（必需），支持 WPS / DOCX / PDF / XLSX |
| `file_type` | 文档类别：`main_body`（正文）、`attachment`（附件）、`doc_basis`（办文依据） |
| `file_name` | 文档名称，用于提示词中的文件标识 |

`file_url` 可以在 JSON 的任意嵌套层级出现（数组、子对象均可），系统会自动递归扫描提取。

## 项目结构

```
doc_flow_agent/
├── main.go                    # 入口
├── internal/
│   ├── config/                # 配置加载
│   ├── model/                 # 数据结构
│   ├── store/                 # SQLite 存储
│   ├── handler/               # HTTP 处理器
│   ├── preprocess/            # 文件预处理
│   ├── llm/                   # LLM 客户端
│   └── logger/                # 日志
├── web/                       # 内嵌 Web 资源
│   ├── templates/             # HTML 模板
│   └── static/                # CSS/JS
├── scripts/                   # 编译脚本
│   ├── build.sh               # Linux 编译
│   ├── build.bat              # Windows 编译
│   ├── Dockerfile             # 编译镜像
│   └── Dockerfile.server      # 生产镜像
└── dist/                      # 编译产物
```