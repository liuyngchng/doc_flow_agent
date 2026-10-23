package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"doc_flow_agent/internal/model"

	_ "modernc.org/sqlite"
)

// SQLiteStore SQLite 元数据存储
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore 创建 SQLite 存储
func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	needsInit := false
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		needsInit = true
	}

	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("打开 SQLite 失败: %w", err)
	}
	db.SetMaxOpenConns(1) // SQLite 单写

	s := &SQLiteStore{db: db}

	if needsInit {
		slog.Info("store_init_database", "path", dbPath)
	}

	if err := s.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("初始化数据库 schema 失败: %w", err)
	}

	return s, nil
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

func (s *SQLiteStore) initSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT UNIQUE NOT NULL,
		password_hash TEXT NOT NULL,
		role INTEGER DEFAULT 0,
		note TEXT DEFAULT '',
		must_change_pwd INTEGER DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS sys_config (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL,
		description TEXT DEFAULT ''
	);

	CREATE TABLE IF NOT EXISTS llm_configs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		api_uri TEXT NOT NULL,
		api_key TEXT NOT NULL,
		model_name TEXT NOT NULL,
		temperature REAL DEFAULT 0.7,
		max_tokens INTEGER DEFAULT 4096,
		is_default INTEGER DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS agent_interfaces (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		path TEXT UNIQUE NOT NULL,
		description TEXT DEFAULT '',
		prompt_template TEXT NOT NULL,
		input_params TEXT DEFAULT '[]',
		output_schema TEXT DEFAULT '{}',
		llm_config_id INTEGER,
		temperature REAL,
		max_tokens INTEGER,
		enabled INTEGER DEFAULT 1,
		version INTEGER DEFAULT 1,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (llm_config_id) REFERENCES llm_configs(id)
	);

	CREATE TABLE IF NOT EXISTS files (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		file_id TEXT UNIQUE NOT NULL,
		original_name TEXT NOT NULL,
		file_type TEXT DEFAULT '',
		file_path TEXT NOT NULL,
		text_content TEXT DEFAULT '',
		text_length INTEGER DEFAULT 0,
		ocr_confidence REAL,
		has_seal INTEGER DEFAULT 0,
		seal_text TEXT DEFAULT '',
		status TEXT DEFAULT 'processing',
		error_message TEXT DEFAULT '',
		biz_id TEXT DEFAULT '',
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
		task_id            TEXT UNIQUE NOT NULL,
		linkid             TEXT DEFAULT '',
		interface_path     TEXT NOT NULL,
		callback_url       TEXT NOT NULL,
		request_body       TEXT NOT NULL,
		status             TEXT NOT NULL DEFAULT 'pending',
		error_message      TEXT DEFAULT '',
		retry_count        INTEGER DEFAULT 0,
		files_text         TEXT DEFAULT '',
		files_summary      TEXT DEFAULT '',
		llm_request_prompt TEXT DEFAULT '',
		llm_raw_output     TEXT DEFAULT '',
		output_result      TEXT DEFAULT '',
		callback_body      TEXT DEFAULT '',
		timeline           TEXT DEFAULT '',
		finished_at        DATETIME DEFAULT NULL,
		created_at         DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at         DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	`
	if _, err := s.db.Exec(schema); err != nil {
		return err
	}

	// 兼容旧表：如果 must_change_pwd 列不存在则添加
	s.db.Exec("ALTER TABLE users ADD COLUMN must_change_pwd INTEGER DEFAULT 0")

	// 兼容旧表：如果 has_seal / seal_text 列不存在则添加
	s.db.Exec("ALTER TABLE files ADD COLUMN has_seal INTEGER DEFAULT 0")
	s.db.Exec("ALTER TABLE files ADD COLUMN seal_text TEXT DEFAULT ''")

	// 创建默认 admin 账号 (admin/admin)
	var count int
	s.db.QueryRow("SELECT COUNT(*) FROM users WHERE username='admin'").Scan(&count)
	if count == 0 {
		// bcrypt hash of "admin"
		defaultHash := "$2a$10$rMQlakR9WSu/Ur2adCYyueoFQ0D66ssd1hB4/KwD/7hEBfpqVoVL6"
		s.db.Exec("INSERT INTO users (username, password_hash, role, note, must_change_pwd) VALUES (?, ?, ?, ?, ?)",
			"admin", defaultHash, model.RoleAdmin, "默认管理员", 1)
		slog.Info("store_default_admin_created")
	}

	return nil
}

// ============================================================
// 用户
// ============================================================

func (s *SQLiteStore) GetUser(userName string) (*model.User, error) {
	u := &model.User{}
	var mustChange int
	err := s.db.QueryRow(
		"SELECT id, username, password_hash, role, note, COALESCE(must_change_pwd,0), COALESCE(created_at,'') FROM users WHERE username=?",
		userName,
	).Scan(&u.ID, &u.UserName, &u.PasswordHash, &u.Role, &u.Note, &mustChange, &u.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	u.MustChangePwd = mustChange == 1
	return u, nil
}

func (s *SQLiteStore) GetUserByID(id int64) (*model.User, error) {
	u := &model.User{}
	var mustChange int
	err := s.db.QueryRow(
		"SELECT id, username, password_hash, role, note, COALESCE(must_change_pwd,0), COALESCE(created_at,'') FROM users WHERE id=?",
		id,
	).Scan(&u.ID, &u.UserName, &u.PasswordHash, &u.Role, &u.Note, &mustChange, &u.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	u.MustChangePwd = mustChange == 1
	return u, nil
}

func (s *SQLiteStore) ListUsers() ([]model.User, error) {
	rows, err := s.db.Query("SELECT id, username, '', role, note, COALESCE(must_change_pwd,0), COALESCE(created_at,'') FROM users ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []model.User
	for rows.Next() {
		var u model.User
		var mustChange int
		if err := rows.Scan(&u.ID, &u.UserName, &u.PasswordHash, &u.Role, &u.Note, &mustChange, &u.CreatedAt); err != nil {
			return nil, err
		}
		u.MustChangePwd = mustChange == 1
		users = append(users, u)
	}
	return users, nil
}

func (s *SQLiteStore) CreateUser(user *model.User) error {
	mustChange := 0
	if user.MustChangePwd {
		mustChange = 1
	}
	_, err := s.db.Exec(
		"INSERT INTO users (username, password_hash, role, note, must_change_pwd) VALUES (?, ?, ?, ?, ?)",
		user.UserName, user.PasswordHash, user.Role, user.Note, mustChange,
	)
	return err
}

func (s *SQLiteStore) DeleteUser(userName string) error {
	_, err := s.db.Exec("DELETE FROM users WHERE username=? AND username != 'admin'", userName)
	return err
}

func (s *SQLiteStore) UpdatePassword(userName string, hash string) error {
	_, err := s.db.Exec("UPDATE users SET password_hash=? WHERE username=?", hash, userName)
	return err
}

// ClearMustChangePwd 清除用户的强制改密标记
func (s *SQLiteStore) ClearMustChangePwd(userName string) error {
	_, err := s.db.Exec("UPDATE users SET must_change_pwd=0 WHERE username=?", userName)
	return err
}

// ============================================================
// 系统配置
// ============================================================

func (s *SQLiteStore) GetConfig(key string) (string, error) {
	var val string
	err := s.db.QueryRow("SELECT value FROM sys_config WHERE key=?", key).Scan(&val)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return val, err
}

func (s *SQLiteStore) SetConfig(key, value, description string) error {
	_, err := s.db.Exec(
		"INSERT INTO sys_config (key, value, description) VALUES (?, ?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value, description=excluded.description",
		key, value, description,
	)
	return err
}

// ============================================================
// LLM 模型配置
// ============================================================

func (s *SQLiteStore) ListLLMConfigs() ([]model.LLMConfig, error) {
	rows, err := s.db.Query(
		"SELECT id, name, api_uri, api_key, model_name, temperature, max_tokens, is_default, COALESCE(created_at,''), COALESCE(updated_at,'') FROM llm_configs ORDER BY id",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var configs []model.LLMConfig
	for rows.Next() {
		var c model.LLMConfig
		if err := rows.Scan(&c.ID, &c.Name, &c.APIURI, &c.APIKey, &c.ModelName,
			&c.Temperature, &c.MaxTokens, &c.IsDefault, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		configs = append(configs, c)
	}
	return configs, nil
}

func (s *SQLiteStore) GetLLMConfig(id int64) (*model.LLMConfig, error) {
	c := &model.LLMConfig{}
	err := s.db.QueryRow(
		"SELECT id, name, api_uri, api_key, model_name, temperature, max_tokens, is_default, COALESCE(created_at,''), COALESCE(updated_at,'') FROM llm_configs WHERE id=?",
		id,
	).Scan(&c.ID, &c.Name, &c.APIURI, &c.APIKey, &c.ModelName, &c.Temperature, &c.MaxTokens, &c.IsDefault, &c.CreatedAt, &c.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (s *SQLiteStore) GetDefaultLLMConfig() (*model.LLMConfig, error) {
	c := &model.LLMConfig{}
	err := s.db.QueryRow(
		"SELECT id, name, api_uri, api_key, model_name, temperature, max_tokens, is_default, COALESCE(created_at,''), COALESCE(updated_at,'') FROM llm_configs WHERE is_default=1 LIMIT 1",
	).Scan(&c.ID, &c.Name, &c.APIURI, &c.APIKey, &c.ModelName, &c.Temperature, &c.MaxTokens, &c.IsDefault, &c.CreatedAt, &c.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (s *SQLiteStore) CreateLLMConfig(cfg *model.LLMConfig) error {
	_, err := s.db.Exec(
		"INSERT INTO llm_configs (name, api_uri, api_key, model_name, temperature, max_tokens, is_default) VALUES (?, ?, ?, ?, ?, ?, ?)",
		cfg.Name, cfg.APIURI, cfg.APIKey, cfg.ModelName, cfg.Temperature, cfg.MaxTokens, boolToInt(cfg.IsDefault),
	)
	return err
}

func (s *SQLiteStore) UpdateLLMConfig(cfg *model.LLMConfig) error {
	_, err := s.db.Exec(
		"UPDATE llm_configs SET name=?, api_uri=?, api_key=?, model_name=?, temperature=?, max_tokens=?, is_default=?, updated_at=CURRENT_TIMESTAMP WHERE id=?",
		cfg.Name, cfg.APIURI, cfg.APIKey, cfg.ModelName, cfg.Temperature, cfg.MaxTokens, boolToInt(cfg.IsDefault), cfg.ID,
	)
	return err
}

func (s *SQLiteStore) DeleteLLMConfig(id int64) error {
	_, err := s.db.Exec("DELETE FROM llm_configs WHERE id=?", id)
	return err
}

func (s *SQLiteStore) SetDefaultLLMConfig(id int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec("UPDATE llm_configs SET is_default=0"); err != nil {
		return err
	}
	if _, err := tx.Exec("UPDATE llm_configs SET is_default=1 WHERE id=?", id); err != nil {
		return err
	}

	return tx.Commit()
}

// ============================================================
// 接口配置
// ============================================================

func (s *SQLiteStore) ListInterfaces() ([]model.InterfaceConfig, error) {
	rows, err := s.db.Query(
		"SELECT id, name, path, description, prompt_template, input_params, output_schema, llm_config_id, temperature, max_tokens, enabled, version, COALESCE(created_at,''), COALESCE(updated_at,'') FROM agent_interfaces ORDER BY id",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanInterfaceConfigs(rows)
}

func (s *SQLiteStore) GetInterfaceByID(id int64) (*model.InterfaceConfig, error) {
	row := s.db.QueryRow(
		"SELECT id, name, path, description, prompt_template, input_params, output_schema, llm_config_id, temperature, max_tokens, enabled, version, COALESCE(created_at,''), COALESCE(updated_at,'') FROM agent_interfaces WHERE id=?",
		id,
	)
	iface, err := scanInterfaceConfig(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return iface, err
}

func (s *SQLiteStore) GetInterfaceByPath(path string) (*model.InterfaceConfig, error) {
	row := s.db.QueryRow(
		"SELECT id, name, path, description, prompt_template, input_params, output_schema, llm_config_id, temperature, max_tokens, enabled, version, COALESCE(created_at,''), COALESCE(updated_at,'') FROM agent_interfaces WHERE path=?",
		path,
	)
	iface, err := scanInterfaceConfig(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return iface, err
}

func (s *SQLiteStore) CreateInterface(iface *model.InterfaceConfig) error {
	paramsJSON, _ := json.Marshal(iface.InputParams)
	_, err := s.db.Exec(
		"INSERT INTO agent_interfaces (name, path, description, prompt_template, input_params, output_schema, llm_config_id, temperature, max_tokens, enabled) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		iface.Name, iface.Path, iface.Description, iface.PromptTemplate, string(paramsJSON),
		iface.OutputSchema, iface.LLMConfigID, iface.Temperature, iface.MaxTokens, boolToInt(iface.Enabled),
	)
	return err
}

func (s *SQLiteStore) UpdateInterface(iface *model.InterfaceConfig) error {
	paramsJSON, _ := json.Marshal(iface.InputParams)
	_, err := s.db.Exec(
		"UPDATE agent_interfaces SET name=?, description=?, prompt_template=?, input_params=?, output_schema=?, llm_config_id=?, temperature=?, max_tokens=?, enabled=?, updated_at=CURRENT_TIMESTAMP WHERE id=?",
		iface.Name, iface.Description, iface.PromptTemplate, string(paramsJSON),
		iface.OutputSchema, iface.LLMConfigID, iface.Temperature, iface.MaxTokens, boolToInt(iface.Enabled), iface.ID,
	)
	return err
}

func (s *SQLiteStore) DeleteInterface(id int64) error {
	_, err := s.db.Exec("DELETE FROM agent_interfaces WHERE id=?", id)
	return err
}

func (s *SQLiteStore) IncrementInterfaceVersion(id int64) error {
	_, err := s.db.Exec("UPDATE agent_interfaces SET version=version+1, updated_at=CURRENT_TIMESTAMP WHERE id=?", id)
	return err
}

// ============================================================
// 文件记录
// ============================================================

func (s *SQLiteStore) CreateFileRecord(record *model.FileRecord) error {
	sealJSON, _ := json.Marshal(record.SealText)
	hasSeal := 0
	if record.HasSeal {
		hasSeal = 1
	}
	_, err := s.db.Exec(
		"INSERT INTO files (file_id, original_name, file_type, file_path, status, biz_id, has_seal, seal_text) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		record.FileID, record.OriginalName, record.FileType, record.FilePath, record.Status, record.BizID, hasSeal, string(sealJSON),
	)
	return err
}

func (s *SQLiteStore) GetFileRecord(fileID string) (*model.FileRecord, error) {
	f := &model.FileRecord{}
	var hasSeal int
	var sealText string
	err := s.db.QueryRow(
		"SELECT id, file_id, original_name, file_type, file_path, COALESCE(text_content,''), text_length, COALESCE(ocr_confidence,0), COALESCE(has_seal,0), COALESCE(seal_text,''), status, COALESCE(error_message,''), COALESCE(biz_id,''), COALESCE(created_at,'') FROM files WHERE file_id=?",
		fileID,
	).Scan(&f.ID, &f.FileID, &f.OriginalName, &f.FileType, &f.FilePath, &f.TextContent,
		&f.TextLength, &f.OCRConfidence, &hasSeal, &sealText, &f.Status, &f.ErrorMessage, &f.BizID, &f.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	f.HasSeal = hasSeal == 1
	if sealText != "" {
		json.Unmarshal([]byte(sealText), &f.SealText)
	}
	return f, nil
}

func (s *SQLiteStore) ListFileRecords(limit, offset int) ([]model.FileRecord, int, error) {
	var total int
	s.db.QueryRow("SELECT COUNT(*) FROM files").Scan(&total)

	rows, err := s.db.Query(
		"SELECT id, file_id, original_name, file_type, file_path, '', text_length, COALESCE(ocr_confidence,0), COALESCE(has_seal,0), COALESCE(seal_text,''), status, COALESCE(error_message,''), COALESCE(biz_id,''), COALESCE(created_at,'') FROM files ORDER BY id DESC LIMIT ? OFFSET ?",
		limit, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var files []model.FileRecord
	for rows.Next() {
		var f model.FileRecord
		var hasSeal int
		var sealText string
		if err := rows.Scan(&f.ID, &f.FileID, &f.OriginalName, &f.FileType, &f.FilePath,
			&f.TextContent, &f.TextLength, &f.OCRConfidence, &hasSeal, &sealText, &f.Status, &f.ErrorMessage, &f.BizID, &f.CreatedAt); err != nil {
			return nil, 0, err
		}
		f.HasSeal = hasSeal == 1
		if sealText != "" {
			json.Unmarshal([]byte(sealText), &f.SealText)
		}
		files = append(files, f)
	}
	return files, total, nil
}

func (s *SQLiteStore) UpdateFileStatus(fileID, status, errMsg string) error {
	_, err := s.db.Exec("UPDATE files SET status=?, error_message=? WHERE file_id=?", status, errMsg, fileID)
	return err
}

func (s *SQLiteStore) UpdateFileParseResult(fileID string, pd *model.ParsedDocument) error {
	sealJSON, _ := json.Marshal(pd.SealText)
	hasSeal := 0
	if pd.HasSeal {
		hasSeal = 1
	}
	_, err := s.db.Exec(
		"UPDATE files SET text_content=?, text_length=?, ocr_confidence=?, has_seal=?, seal_text=?, status='ready' WHERE file_id=?",
		pd.Text, len(pd.Text), pd.OCRConfidence, hasSeal, string(sealJSON), fileID,
	)
	return err
}

func (s *SQLiteStore) DeleteFileRecord(fileID string) error {
	_, err := s.db.Exec("DELETE FROM files WHERE file_id=?", fileID)
	return err
}

// ============================================================
// 任务追踪
// ============================================================

func (s *SQLiteStore) CreateTaskTrace(task *model.TaskTrace) error {
	_, err := s.db.Exec(
		"INSERT INTO task_trace (task_id, linkid, interface_path, callback_url, request_body, status, files_text, files_summary, llm_request_prompt, llm_raw_output, output_result, callback_body, timeline, finished_at, retry_count, error_message) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		task.TaskID, task.LinkID, task.InterfacePath, task.CallbackURL, task.RequestBody, task.Status, task.FilesText, task.FilesSummary, task.LLMRequestPrompt, task.LLMRawOutput, task.OutputResult, task.CallbackBody, task.Timeline, nilIfEmpty(task.FinishedAt), task.RetryCount, task.ErrorMessage,
	)
	return err
}

func (s *SQLiteStore) UpdateTaskTrace(task *model.TaskTrace) error {
	_, err := s.db.Exec(
		"UPDATE task_trace SET status=?, error_message=?, retry_count=?, files_text=?, files_summary=?, llm_request_prompt=?, llm_raw_output=?, output_result=?, callback_body=?, timeline=?, finished_at=?, updated_at=CURRENT_TIMESTAMP WHERE task_id=?",
		task.Status, task.ErrorMessage, task.RetryCount, task.FilesText, task.FilesSummary, task.LLMRequestPrompt, task.LLMRawOutput, task.OutputResult, task.CallbackBody, task.Timeline, nilIfEmpty(task.FinishedAt), task.TaskID,
	)
	return err
}

func (s *SQLiteStore) GetTaskTrace(taskID string) (*model.TaskTrace, error) {
	t := &model.TaskTrace{}
	var finishedAt sql.NullString
	err := s.db.QueryRow(
		"SELECT id, task_id, COALESCE(linkid,''), interface_path, callback_url, request_body, status, COALESCE(error_message,''), retry_count, COALESCE(files_text,''), COALESCE(files_summary,''), COALESCE(llm_request_prompt,''), COALESCE(llm_raw_output,''), COALESCE(output_result,''), COALESCE(callback_body,''), COALESCE(timeline,''), finished_at, COALESCE(created_at,''), COALESCE(updated_at,'') FROM task_trace WHERE task_id=?",
		taskID,
	).Scan(&t.ID, &t.TaskID, &t.LinkID, &t.InterfacePath, &t.CallbackURL, &t.RequestBody, &t.Status, &t.ErrorMessage, &t.RetryCount, &t.FilesText, &t.FilesSummary, &t.LLMRequestPrompt, &t.LLMRawOutput, &t.OutputResult, &t.CallbackBody, &t.Timeline, &finishedAt, &t.CreatedAt, &t.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if finishedAt.Valid {
		t.FinishedAt = finishedAt.String
	}
	return t, nil
}

func (s *SQLiteStore) DeleteTaskTrace(taskID string) error {
	_, err := s.db.Exec("DELETE FROM task_trace WHERE task_id=?", taskID)
	return err
}

func (s *SQLiteStore) ListRunningTasks() ([]model.TaskTrace, error) {
	rows, err := s.db.Query(
		"SELECT id, task_id, COALESCE(linkid,''), interface_path, callback_url, request_body, status, COALESCE(error_message,''), retry_count, COALESCE(files_text,''), COALESCE(files_summary,''), COALESCE(llm_request_prompt,''), COALESCE(llm_raw_output,''), COALESCE(output_result,''), COALESCE(callback_body,''), COALESCE(timeline,''), finished_at, COALESCE(created_at,''), COALESCE(updated_at,'') FROM task_trace WHERE status IN ('pending','processing') ORDER BY id",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanTaskTraces(rows)
}

func (s *SQLiteStore) ListTaskTraces(limit, offset int) ([]model.TaskTrace, int, error) {
	var total int
	s.db.QueryRow("SELECT COUNT(*) FROM task_trace").Scan(&total)

	rows, err := s.db.Query(
		"SELECT id, task_id, COALESCE(linkid,''), interface_path, callback_url, request_body, status, COALESCE(error_message,''), retry_count, COALESCE(files_text,''), COALESCE(files_summary,''), COALESCE(llm_request_prompt,''), COALESCE(llm_raw_output,''), COALESCE(output_result,''), COALESCE(callback_body,''), COALESCE(timeline,''), finished_at, COALESCE(created_at,''), COALESCE(updated_at,'') FROM task_trace ORDER BY id DESC LIMIT ? OFFSET ?",
		limit, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	tasks, err := scanTaskTraces(rows)
	return tasks, total, err
}

func (s *SQLiteStore) CountRunningTasks() (int, error) {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM task_trace WHERE status IN ('pending','processing')").Scan(&count)
	return count, err
}

// PruneOldRecords 清理超过 retentionDays 天的运行时数据，返回被删文件的磁盘路径列表
// 清理范围：task_trace（终态）、files（含用户上传和临时下载）
// 不动：users、sys_config、llm_configs、agent_interfaces、api_tokens
func (s *SQLiteStore) PruneOldRecords(retentionDays int) ([]string, error) {
	cutoff := fmt.Sprintf("-%d days", retentionDays)
	if retentionDays <= 0 {
		retentionDays = 30
		cutoff = "-30 days"
	}

	// 1. 先查出待删文件的磁盘路径（DB 删除前）
	var filePaths []string
	rows, err := s.db.Query("SELECT file_path FROM files WHERE file_path != '' AND created_at < datetime('now', ?)", cutoff)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var p string
			rows.Scan(&p)
			if p != "" {
				filePaths = append(filePaths, p)
			}
		}
	}

	// 2. task_trace：只删终态
	res, err := s.db.Exec(
		"DELETE FROM task_trace WHERE status IN ('success','failed','completed','callback_fail') AND updated_at < datetime('now', ?)",
		cutoff,
	)
	if err != nil {
		return nil, fmt.Errorf("清理 task_trace 失败: %w", err)
	}
	nTasks, _ := res.RowsAffected()

	// 3. files
	res, err = s.db.Exec("DELETE FROM files WHERE created_at < datetime('now', ?)", cutoff)
	if err != nil {
		return nil, fmt.Errorf("清理 files 失败: %w", err)
	}
	nFiles, _ := res.RowsAffected()

	slog.Info("prune_records",
		"retention_days", retentionDays,
		"deleted_task_traces", nTasks,
		"deleted_files", nFiles,
		"disk_paths_to_remove", len(filePaths),
	)

	return filePaths, nil
}

// ============================================================
// 辅助函数
// ============================================================

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nilIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

func scanTaskTraces(rows *sql.Rows) ([]model.TaskTrace, error) {
	var tasks []model.TaskTrace
	for rows.Next() {
		var t model.TaskTrace
		var finishedAt sql.NullString
		if err := rows.Scan(&t.ID, &t.TaskID, &t.LinkID, &t.InterfacePath, &t.CallbackURL, &t.RequestBody, &t.Status, &t.ErrorMessage, &t.RetryCount, &t.FilesText, &t.FilesSummary, &t.LLMRequestPrompt, &t.LLMRawOutput, &t.OutputResult, &t.CallbackBody, &t.Timeline, &finishedAt, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		if finishedAt.Valid {
			t.FinishedAt = finishedAt.String
		}
		tasks = append(tasks, t)
	}
	return tasks, nil
}

func scanInterfaceConfig(scanner interface{ Scan(...interface{}) error }) (*model.InterfaceConfig, error) {
	var iface model.InterfaceConfig
	var paramsJSON string
	var enabledInt int

	err := scanner.Scan(
		&iface.ID, &iface.Name, &iface.Path, &iface.Description, &iface.PromptTemplate,
		&paramsJSON, &iface.OutputSchema, &iface.LLMConfigID, &iface.Temperature, &iface.MaxTokens,
		&enabledInt, &iface.Version, &iface.CreatedAt, &iface.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	iface.Enabled = enabledInt == 1

	if paramsJSON != "" && paramsJSON != "[]" {
		paramsJSON = strings.TrimSpace(paramsJSON)
		if paramsJSON != "" {
			json.Unmarshal([]byte(paramsJSON), &iface.InputParams)
		}
	}
	if iface.InputParams == nil {
		iface.InputParams = []model.InputParam{}
	}

	return &iface, nil
}

func scanInterfaceConfigs(rows *sql.Rows) ([]model.InterfaceConfig, error) {
	var ifaces []model.InterfaceConfig
	for rows.Next() {
		var iface model.InterfaceConfig
		var paramsJSON string
		var enabledInt int

		if err := rows.Scan(
			&iface.ID, &iface.Name, &iface.Path, &iface.Description, &iface.PromptTemplate,
			&paramsJSON, &iface.OutputSchema, &iface.LLMConfigID, &iface.Temperature, &iface.MaxTokens,
			&enabledInt, &iface.Version, &iface.CreatedAt, &iface.UpdatedAt,
		); err != nil {
			return nil, err
		}

		iface.Enabled = enabledInt == 1

		if paramsJSON != "" && paramsJSON != "[]" {
			paramsJSON = strings.TrimSpace(paramsJSON)
			if paramsJSON != "" {
				json.Unmarshal([]byte(paramsJSON), &iface.InputParams)
			}
		}
		if iface.InputParams == nil {
			iface.InputParams = []model.InputParam{}
		}

		ifaces = append(ifaces, iface)
	}
	return ifaces, nil
}