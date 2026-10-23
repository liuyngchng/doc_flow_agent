# doc_flow_agent — 电子公文流转智能体

面向电子公文工作流的通用 AI 网关，将文件预处理与可配置的 LLM 调用接口解耦。对外提供异步 AI 接口，电子公文系统提交任务后立即返回，处理完成后通过回调返回结果。

## 功能

- **文件预处理**：DOCX / 文字 PDF / XLSX 文本提取，支持印章检测
- **可配置 LLM 接口**：管理员在 GUI 中配置提示词模板、输出 Schema，自动生成对外 API
- **异步任务处理**：接收请求后立即返回 `{code, task_id, linkid}`，后台 Worker 下载文件→调用 LLM→回调结果
- **任务管理**：支持查询进行中任务、取消排队中任务
- **管理后台**：登录、AI 接口管理、LLM 模型配置、文件管理、调用日志、审稿模拟

## 架构

```
电子公文系统 ──POST /api/v1/*──► doc_flow_agent ──► LLM API
    ▲                              │
    │                              ├── 文件预处理 (docx/pdf/xlsx 文本提取 + 印章检测)
    │                              ├── WorkerPool 异步消费（download → parse → LLM → callback）
    └──────── POST callback_url ◄──┘
                                   │
                                   ├── Admin GUI (登录 + 接口/模型/文件/日志 CRUD)
                                   └── GET/DELETE /api/v1/tasks (任务查询与取消)
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

## 对外 API（电子公文系统调用）

所有业务接口（IF-02/03/04/06）为**异步模式**：电子公文系统提交任务后立即返回 `{code, task_id, linkid}`，智能体后台处理后通过 `callback_url` 回调返回结果。

### 1. 调用 AI 接口（异步）

```bash
# IF-02 收文分类 示例
curl -k -X POST https://11.14.2.27:20001/api/v1/document/classify \
  -H "Content-Type: application/json" \
  -H "X-Api-Key: edoc_sk_xxxxxxxxxxxx" \
  -d '{
    "linkid": "doc_20261020_0001",
    "callback_url": "https://edoc.example.com/api/v1/callback",
    "process_type": "公司收文",
    "form_data": {"title": "关于安全检查的通知"},
    "files": [
      {"file_url": "https://edoc.example.com/api/v1/files/body_001", "file_type": "main_body"}
    ],
    "next_nodes": [{"id": "dept_zhgl", "name": "综合管理部", "type": "org"}],
    "current_step": "收文登记",
    "is_exclusive": true
  }'

# 立即返回:
# {"code": 0, "message": "success", "task_id": "a1b2c3d4-e5f6-...", "linkid": "doc_20261020_0001"}
```

接口路径通过管理后台配置创建，所有 `POST /api/v1/*` 统一走异步处理流程。

### 2. 查询进行中任务

```bash
curl -k -X GET https://11.14.2.27:20001/api/v1/tasks \
  -H "X-Api-Key: edoc_sk_xxxxxxxxxxxx"

# 返回:
# {"code": 0, "message": "success", "data": {"total": 2, "max_concurrent": 5, "tasks": [...]}}
```

### 3. 取消排队中任务

```bash
curl -k -X DELETE https://11.14.2.27:20001/api/v1/tasks/doc_20261020_0001 \
  -H "X-Api-Key: edoc_sk_xxxxxxxxxxxx"

# 返回:
# {"code": 0, "message": "任务已取消", "task_id": "doc_20261020_0001"}
```

> 仅 `pending` 状态的任务可取消；`processing`（LLM 调用中）状态不可取消。

### 4. 健康检查

```bash
curl -k --tlsv1 --noproxy '*' -w'\n' https://11.14.2.27:20001/health
# 返回: {"status":"ok"}
```

### 5. 文件上传（回传）

```bash
curl -k -X POST https://11.14.2.27:20001/api/files/upload \
  -H "X-Api-Key: agent_sk_xxxxxxxxxxxx" \
  -F "linkid=doc_20261020_0003" \
  -F "file=@annotated.png" \
  -F "file_name=简报标注.png" \
  -F "file_type=main_body"
# 返回: {"code": 0, "message": "success", "data": {"file_id": "file_xxx", "file_url": "https://..."}}
```

## 管理后台 API

| 方法 | 路径 | 说明 |
|------|------|------|
| GET/POST | `/api/inner/interfaces` | AI 接口列表/创建 |
| GET | `/api/inner/interfaces/:id` | 获取接口详情 |
| PUT/DELETE | `/api/inner/interfaces/:id` | 更新/删除接口 |
| POST | `/api/inner/interfaces/:id/test` | 测试接口 prompt 渲染 |
| GET/POST | `/api/inner/llm-configs` | LLM 配置列表/创建 |
| PUT/DELETE | `/api/inner/llm-configs/:id` | 更新/删除配置 |
| PUT | `/api/inner/llm-configs/:id/default` | 设为默认 |
| GET | `/api/inner/files` | 文件列表 |
| DELETE | `/api/inner/files/:id` | 删除文件 |
| GET | `/api/inner/call-logs` | 调用日志 |
| GET/PUT | `/api/inner/config` | 系统配置 |
| POST | `/api/inner/proofread/submit` | 审稿模拟 |
| GET | `/api/inner/proofread/query/:task_id` | 审稿查询 |

## 接口工作流程

管理员在后台创建 AI 接口（配置接口路径、提示词模板、输出 Schema）后，对外的 `POST /api/v1/*` 按以下流程处理：

### 第一步：提交受理（同步，毫秒级返回）

- 校验必填字段 `linkid`、`callback_url`
- 检查当前处理中任务数是否达上限 → 超限返回 `{code: 2002}`
- 创建异步任务记录（status=pending），入队 Worker Pool
- **立即返回** `{code: 0, task_id, linkid}`（task_id 为智能体自生成 UUID，linkid 为对方业务流程标识）

### 第二步：后台处理（Worker 异步执行）

- 更新任务状态为 `processing`
- 下载请求 JSON 中所有 `file_url` 对应的文件
- 调用文档解析服务（`cfg.yml` 中 `doc_parser.url`）或内置解析器提取文本 + 印章信息
- 按数据字典（`cfg.yml` 中 `dict`）翻译请求 JSON 的 key 为中文
- 按接口配置的 `prompt_template` 模板，替换占位符：
  - `{{request.body}}` — 原始请求 JSON
  - `{{request.dict}}` — 翻译成中文 key 后的 JSON
  - `{{file.main_body}}` — 正文文档全文
  - `{{file.attachments}}` — 附件文档全文
  - `{{file.doc_basis}}` — 办文依据文档全文
  - `{{file.seal_text}}` — 所有印章文字汇总
  - `{{file.all_text}}` — 全部文档文本（分章节）
- 调用 LLM（OpenAI 兼容接口），要求按 Schema 返回 JSON
- 记录 LLM 输出结果

### 第三步：回调结果

- 向 `callback_url` 发起 POST，Body 含完整的业务处理结果
- 认证：`Authorization: Bearer {callback_url_token}`（token 在 `cfg.yml` 配置）
- HTTPS 回调跳过证书验证（兼容自签证书）
- 回调失败自动重试（默认 3 次，指数退避），重试耗尽后 ERROR 日志打印完整返回内容
- 回调成功 → 任务状态 `completed`；失败耗尽 → 状态 `callback_fail`

### 任务状态机

```
pending → processing → success/failed → completed/callback_fail
```

---

## 文件引用约定

电子公文系统在请求 JSON 中通过 `files` 数组引用待解析的文件，系统自动递归扫描下载：

```json
{
  "files": [
    {"file_url": "https://wps.example.com/files/xxx.docx", "file_type": "main_body"},
    {"file_url": "https://wps.example.com/files/yyy.xlsx", "file_type": "attachment", "file_name": "附件清单.xlsx"},
    {"file_url": "https://wps.example.com/files/zzz.pdf",  "file_type": "doc_basis", "file_name": "会议纪要.pdf"}
  ]
}
```

| 字段 | 说明 |
|------|------|
| `file_url` | 文档下载链接（必需），支持 DOCX / PDF / XLSX / WPS |
| `file_type` | 文档类别：`main_body`（正文）、`attachment`（附件）、`doc_basis`（办文依据） |
| `file_name` | 文档名称（可选），用于提示词中的文件标识 |
| `format` | 文件格式后缀（可选），如 `pdf`、`docx` |

> `files` 数组可以在 JSON 的任意嵌套层级出现，系统会自动递归扫描提取所有 `file_url`。

## 项目结构

```
doc_flow_agent/
├── main.go                    # 入口（路由 + Worker 生命周期）
├── internal/
│   ├── config/                # 配置加载
│   ├── model/                 # 数据结构
│   ├── store/                 # SQLite 存储
│   ├── handler/               # HTTP 处理器（含异步 Worker）
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