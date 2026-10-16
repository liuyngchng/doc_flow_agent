# DocAI Gateway - 文档 AI 智能网关

面向 电子公文工作流的通用 AI 网关，将文件预处理与可配置的 LLM 调用接口解耦。

## 功能

- **文件预处理**：DOCX / 文字 PDF / XLSX 文本提取（扫描件 OCR 可后续扩展）
- **可配置 LLM 接口**：管理员在 GUI 中配置提示词模板、输入占位符、输出 Schema
- **通用 AI 调用**：电子公文工作流上传文件一次，通过 `file_id` 多次调用不同 AI 接口
- **管理后台**：登录、AI 接口管理、LLM 模型配置、文件管理、调用日志、模拟公文处理

## 架构

```
电子公文工作流 ──HTTP──> doc-ai-gateway ──> LLM API
                     │
                     ├── 文件预处理 (docx/pdf/xlsx 文本提取)
                     ├── 可配置 AI 接口 (提示词模板 + 占位符 + Schema)
                     └── Admin GUI (登录 + CRUD + 日志)
```

## 快速开始

### Windows

```bash
scripts/build.bat          # 一键编译
cd dist/doc-ai-gateway-windows-amd64
doc-ai-gateway.exe         # 双击运行
```

访问 http://localhost:8080/admin ，默认账号 `admin` / `admin`。

> **注意**：admin 首次登录必须修改密码，**2 小时内未修改则初始密码作废**，需删库重建。

### Linux / Docker

```bash
scripts/build.sh           # 编译 + 构建 Docker 镜像
docker run -p 8080:8080 -v $(pwd)/data:/app/data doc-ai-gateway:latest
```

## 对外 API（电子公文工作流调用）

### 1. 上传文件

```bash
curl -X POST http://localhost:8080/api/files/upload \
  -F "file=@test.pdf" \
  -F "biz_id=FLOW-001"
# 返回: {"file_id":"F-20261027001","status":"processing",...}
```

### 2. 查询文件状态

```bash
curl http://localhost:8080/api/files/status/F-20261027001
```

### 3. 调用 AI 接口

```bash
curl -X POST http://localhost:8080/api/ai/call \
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
| GET/POST | `/api/v1/interfaces` | AI 接口列表/创建 |
| PUT/DELETE | `/api/v1/interfaces/:id` | 更新/删除接口 |
| POST | `/api/v1/interfaces/:id/test` | 测试接口 |
| GET/POST | `/api/v1/llm-configs` | LLM 配置列表/创建 |
| PUT/DELETE | `/api/v1/llm-configs/:id` | 更新/删除配置 |
| PUT | `/api/v1/llm-configs/:id/default` | 设为默认 |
| GET | `/api/v1/files` | 文件列表 |
| DELETE | `/api/v1/files/:id` | 删除文件 |
| GET | `/api/v1/call-logs` | 调用日志 |

## 提示词模板占位符

| 占位符 | 说明 |
|--------|------|
| `{{file.text}}` | 文件预处理后的纯文本全文 |
| `{{file.name}}` | 原始文件名 |
| `{{file.type}}` | 文件类型 (docx/pdf/xlsx) |
| `{{params.xxx}}` | 电子公文工作流调用时传入的自定义参数 |

## 项目结构

```
doc-ai-gateway/
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