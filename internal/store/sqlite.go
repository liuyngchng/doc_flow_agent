package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"doc-ai-gateway/internal/model"

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

	CREATE TABLE IF NOT EXISTS ai_interfaces (
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
		status TEXT DEFAULT 'processing',
		error_message TEXT DEFAULT '',
		biz_id TEXT DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS call_logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		file_id TEXT NOT NULL,
		interface_id INTEGER DEFAULT 0,
		interface_path TEXT NOT NULL,
		input_params TEXT DEFAULT '{}',
		output_result TEXT DEFAULT '{}',
		status TEXT DEFAULT 'success',
		error_message TEXT DEFAULT '',
		elapsed_ms INTEGER DEFAULT 0,
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
	`
	if _, err := s.db.Exec(schema); err != nil {
		return err
	}

	// 兼容旧表：如果 must_change_pwd 列不存在则添加
	s.db.Exec("ALTER TABLE users ADD COLUMN must_change_pwd INTEGER DEFAULT 0")

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
// AI 接口配置
// ============================================================

func (s *SQLiteStore) ListAIInterfaces() ([]model.AIInterface, error) {
	rows, err := s.db.Query(
		"SELECT id, name, path, description, prompt_template, input_params, output_schema, llm_config_id, temperature, max_tokens, enabled, version, COALESCE(created_at,''), COALESCE(updated_at,'') FROM ai_interfaces ORDER BY id",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanAIInterfaces(rows)
}

func (s *SQLiteStore) GetAIInterfaceByID(id int64) (*model.AIInterface, error) {
	row := s.db.QueryRow(
		"SELECT id, name, path, description, prompt_template, input_params, output_schema, llm_config_id, temperature, max_tokens, enabled, version, COALESCE(created_at,''), COALESCE(updated_at,'') FROM ai_interfaces WHERE id=?",
		id,
	)
	iface, err := scanAIInterface(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return iface, err
}

func (s *SQLiteStore) GetAIInterfaceByPath(path string) (*model.AIInterface, error) {
	row := s.db.QueryRow(
		"SELECT id, name, path, description, prompt_template, input_params, output_schema, llm_config_id, temperature, max_tokens, enabled, version, COALESCE(created_at,''), COALESCE(updated_at,'') FROM ai_interfaces WHERE path=?",
		path,
	)
	iface, err := scanAIInterface(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return iface, err
}

func (s *SQLiteStore) CreateAIInterface(iface *model.AIInterface) error {
	paramsJSON, _ := json.Marshal(iface.InputParams)
	_, err := s.db.Exec(
		"INSERT INTO ai_interfaces (name, path, description, prompt_template, input_params, output_schema, llm_config_id, temperature, max_tokens, enabled) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		iface.Name, iface.Path, iface.Description, iface.PromptTemplate, string(paramsJSON),
		iface.OutputSchema, iface.LLMConfigID, iface.Temperature, iface.MaxTokens, boolToInt(iface.Enabled),
	)
	return err
}

func (s *SQLiteStore) UpdateAIInterface(iface *model.AIInterface) error {
	paramsJSON, _ := json.Marshal(iface.InputParams)
	_, err := s.db.Exec(
		"UPDATE ai_interfaces SET name=?, description=?, prompt_template=?, input_params=?, output_schema=?, llm_config_id=?, temperature=?, max_tokens=?, enabled=?, updated_at=CURRENT_TIMESTAMP WHERE id=?",
		iface.Name, iface.Description, iface.PromptTemplate, string(paramsJSON),
		iface.OutputSchema, iface.LLMConfigID, iface.Temperature, iface.MaxTokens, boolToInt(iface.Enabled), iface.ID,
	)
	return err
}

func (s *SQLiteStore) DeleteAIInterface(id int64) error {
	_, err := s.db.Exec("DELETE FROM ai_interfaces WHERE id=?", id)
	return err
}

func (s *SQLiteStore) IncrementInterfaceVersion(id int64) error {
	_, err := s.db.Exec("UPDATE ai_interfaces SET version=version+1, updated_at=CURRENT_TIMESTAMP WHERE id=?", id)
	return err
}

// ============================================================
// 文件记录
// ============================================================

func (s *SQLiteStore) CreateFileRecord(record *model.FileRecord) error {
	_, err := s.db.Exec(
		"INSERT INTO files (file_id, original_name, file_type, file_path, status, biz_id) VALUES (?, ?, ?, ?, ?, ?)",
		record.FileID, record.OriginalName, record.FileType, record.FilePath, record.Status, record.BizID,
	)
	return err
}

func (s *SQLiteStore) GetFileRecord(fileID string) (*model.FileRecord, error) {
	f := &model.FileRecord{}
	err := s.db.QueryRow(
		"SELECT id, file_id, original_name, file_type, file_path, COALESCE(text_content,''), text_length, COALESCE(ocr_confidence,0), status, COALESCE(error_message,''), COALESCE(biz_id,''), COALESCE(created_at,'') FROM files WHERE file_id=?",
		fileID,
	).Scan(&f.ID, &f.FileID, &f.OriginalName, &f.FileType, &f.FilePath, &f.TextContent,
		&f.TextLength, &f.OCRConfidence, &f.Status, &f.ErrorMessage, &f.BizID, &f.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return f, nil
}

func (s *SQLiteStore) ListFileRecords(limit, offset int) ([]model.FileRecord, int, error) {
	var total int
	s.db.QueryRow("SELECT COUNT(*) FROM files").Scan(&total)

	rows, err := s.db.Query(
		"SELECT id, file_id, original_name, file_type, file_path, '', text_length, COALESCE(ocr_confidence,0), status, COALESCE(error_message,''), COALESCE(biz_id,''), COALESCE(created_at,'') FROM files ORDER BY id DESC LIMIT ? OFFSET ?",
		limit, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var files []model.FileRecord
	for rows.Next() {
		var f model.FileRecord
		if err := rows.Scan(&f.ID, &f.FileID, &f.OriginalName, &f.FileType, &f.FilePath,
			&f.TextContent, &f.TextLength, &f.OCRConfidence, &f.Status, &f.ErrorMessage, &f.BizID, &f.CreatedAt); err != nil {
			return nil, 0, err
		}
		files = append(files, f)
	}
	return files, total, nil
}

func (s *SQLiteStore) UpdateFileStatus(fileID, status, errMsg string) error {
	_, err := s.db.Exec("UPDATE files SET status=?, error_message=? WHERE file_id=?", status, errMsg, fileID)
	return err
}

func (s *SQLiteStore) UpdateFileText(fileID, text string, length int, ocrConf float64) error {
	_, err := s.db.Exec(
		"UPDATE files SET text_content=?, text_length=?, ocr_confidence=?, status='ready' WHERE file_id=?",
		text, length, ocrConf, fileID,
	)
	return err
}

func (s *SQLiteStore) DeleteFileRecord(fileID string) error {
	_, err := s.db.Exec("DELETE FROM files WHERE file_id=?", fileID)
	return err
}

// ============================================================
// 调用日志
// ============================================================

func (s *SQLiteStore) CreateCallLog(log *model.CallLog) error {
	_, err := s.db.Exec(
		"INSERT INTO call_logs (file_id, interface_id, interface_path, input_params, output_result, status, error_message, elapsed_ms) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		log.FileID, log.InterfaceID, log.InterfacePath, log.InputParams, log.OutputResult, log.Status, log.ErrorMessage, log.ElapsedMs,
	)
	return err
}

func (s *SQLiteStore) ListCallLogs(fileID string, limit, offset int) ([]model.CallLog, int, error) {
	var total int
	var rows *sql.Rows
	var err error

	if fileID != "" {
		s.db.QueryRow("SELECT COUNT(*) FROM call_logs WHERE file_id=?", fileID).Scan(&total)
		rows, err = s.db.Query(
			"SELECT id, file_id, COALESCE(interface_id,0), interface_path, input_params, output_result, status, COALESCE(error_message,''), elapsed_ms, COALESCE(created_at,'') FROM call_logs WHERE file_id=? ORDER BY id DESC LIMIT ? OFFSET ?",
			fileID, limit, offset,
		)
	} else {
		s.db.QueryRow("SELECT COUNT(*) FROM call_logs").Scan(&total)
		rows, err = s.db.Query(
			"SELECT id, file_id, COALESCE(interface_id,0), interface_path, input_params, output_result, status, COALESCE(error_message,''), elapsed_ms, COALESCE(created_at,'') FROM call_logs ORDER BY id DESC LIMIT ? OFFSET ?",
			limit, offset,
		)
	}
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var logs []model.CallLog
	for rows.Next() {
		var l model.CallLog
		if err := rows.Scan(&l.ID, &l.FileID, &l.InterfaceID, &l.InterfacePath, &l.InputParams,
			&l.OutputResult, &l.Status, &l.ErrorMessage, &l.ElapsedMs, &l.CreatedAt); err != nil {
			return nil, 0, err
		}
		logs = append(logs, l)
	}
	return logs, total, nil
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

func scanAIInterface(scanner interface{ Scan(...interface{}) error }) (*model.AIInterface, error) {
	var iface model.AIInterface
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

func scanAIInterfaces(rows *sql.Rows) ([]model.AIInterface, error) {
	var ifaces []model.AIInterface
	for rows.Next() {
		var iface model.AIInterface
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