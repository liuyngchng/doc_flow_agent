-- ================================================================
-- doc_flow_agent — SQLite 数据库表结构参考
-- ================================================================
-- 说明：本文件仅供查看数据库结构，程序运行时不使用它。
-- 程序首次启动会自动创建 cfg.db 并初始化以下所有表，
-- 同时插入默认管理员账号 admin / admin（首次登录需修改密码）。
-- ================================================================

CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    role INTEGER DEFAULT 0,            -- 0=普通用户 1=管理员
    note TEXT DEFAULT '',
    must_change_pwd INTEGER DEFAULT 0,  -- 1=首次登录需强制修改密码
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS sys_config (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    description TEXT DEFAULT ''
);

CREATE TABLE IF NOT EXISTS llm_configs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,                 -- 配置名称，如 "DeepSeek V3"
    api_uri TEXT NOT NULL,              -- API 地址
    api_key TEXT NOT NULL,              -- API Key
    model_name TEXT NOT NULL,           -- 模型名称
    temperature REAL DEFAULT 0.7,
    max_tokens INTEGER DEFAULT 4096,
    is_default INTEGER DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- agent_interfaces 为旧版接口配置表（逐步废弃，保留向后兼容）
CREATE TABLE IF NOT EXISTS agent_interfaces (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,                 -- 接口名称
    path TEXT UNIQUE NOT NULL,          -- 接口路径
    description TEXT DEFAULT '',
    prompt_template TEXT NOT NULL,      -- 提示词模板，含 {{占位符}}
    input_params TEXT DEFAULT '[]',     -- JSON 参数定义
    output_schema TEXT DEFAULT '{}',    -- JSON Schema 约束输出格式
    llm_config_id INTEGER,              -- 关联的 LLM 模型配置
    temperature REAL,                   -- 可为 NULL（用默认值）
    max_tokens INTEGER,
    enabled INTEGER DEFAULT 1,
    version INTEGER DEFAULT 1,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (llm_config_id) REFERENCES llm_configs(id)
);

-- agent_templates 为新的提示词模板表（替代 agent_interfaces 的页面功能）
-- 页面上创建模板时必须关联一个接口路径（interface_path），
-- 接口路径由代码硬编码（见 internal/interfaces/registry.go）
CREATE TABLE IF NOT EXISTS agent_templates (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    name            TEXT NOT NULL,        -- 模板标题，如"区分公文类型"
    interface_path  TEXT NOT NULL DEFAULT '', -- 关联的接口路径，如 document/classify
    description     TEXT DEFAULT '',      -- 用途说明
    prompt_template TEXT NOT NULL,        -- 提示词内容，含 {{占位符}}
    output_schema   TEXT DEFAULT '{}',    -- 输出 JSON Schema
    llm_config_id   INTEGER,             -- 关联的 LLM 模型配置
    temperature     REAL,                -- 可为 NULL（用默认值）
    max_tokens      INTEGER,
    enabled         INTEGER DEFAULT 1,
    version         INTEGER DEFAULT 1,
    created_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (llm_config_id) REFERENCES llm_configs(id)
);

CREATE TABLE IF NOT EXISTS files (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    file_id TEXT UNIQUE NOT NULL,       -- 业务标识
    original_name TEXT NOT NULL,
    file_type TEXT DEFAULT '',          -- docx/pdf/xlsx 等
    file_path TEXT NOT NULL,            -- 本地存储路径
    text_content TEXT DEFAULT '',       -- 预处理提取的文本
    text_length INTEGER DEFAULT 0,
    ocr_confidence REAL,
    has_seal INTEGER DEFAULT 0,         -- 是否检测到印章
    seal_text TEXT DEFAULT '',          -- 印章文字列表 (JSON array)
    status TEXT DEFAULT 'processing',   -- processing/ready/failed
    error_message TEXT DEFAULT '',
    biz_id TEXT DEFAULT '',             -- 公文流程实例 ID
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS api_tokens (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    token TEXT UNIQUE NOT NULL,
    user_id INTEGER,
    description TEXT DEFAULT '',
    enabled INTEGER DEFAULT 1,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id)
);

CREATE TABLE IF NOT EXISTS task_trace (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id            TEXT UNIQUE NOT NULL,         -- 智能体自生成 UUID，全局唯一
    linkid             TEXT DEFAULT '',              -- 对方系统的流程标识
    interface_path     TEXT NOT NULL,                -- 对应的 AI 接口路径
    callback_url       TEXT NOT NULL,                -- 回调地址
    request_body       TEXT NOT NULL,                -- 对方 POST 的原始 JSON
    status             TEXT NOT NULL DEFAULT 'pending', -- pending/processing/success/failed/completed/callback_fail
    error_message      TEXT DEFAULT '',              -- 错误信息
    retry_count        INTEGER DEFAULT 0,            -- 回调重试次数
    files_text         TEXT DEFAULT '',              -- 文件全文+元信息（JSON 数组）
    files_summary      TEXT DEFAULT '',              -- 文件处理摘要（JSON 数组）
    llm_request_prompt TEXT DEFAULT '',              -- 模板渲染后的完整 user prompt
    llm_raw_output     TEXT DEFAULT '',              -- LLM 返回的原始文本（wrap 之前）
    output_result      TEXT DEFAULT '',              -- 包装后的最终输出
    callback_body      TEXT DEFAULT '',              -- 发送给 callback_url 的完整请求体
    timeline           TEXT DEFAULT '',              -- 各环节时间线（JSON 数组）
    finished_at        DATETIME DEFAULT NULL,         -- 进入终态的时间
    created_at         DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at         DATETIME DEFAULT CURRENT_TIMESTAMP
);
