package handler

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"doc-ai-gateway/internal/model"
	"doc-ai-gateway/internal/store"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

// AuthHandler 认证处理器
type AuthHandler struct {
	cfg          *model.Config
	store        store.MetaStore
	jwtKey       []byte
	keyTemporary bool // 临时密钥：token 仅 2h 有效，admin 改密码后持久化到 DB
}

// NewAuthHandler 创建认证处理器
// 密钥优先级：DB 持久化密钥 > 自动生成临时密钥（2h 有效，admin 改密码后持久化）
func NewAuthHandler(cfg *model.Config, st store.MetaStore) *AuthHandler {
	keyTemporary := false
	var key []byte

	// 尝试从 DB 加载之前持久化的密钥（admin 改密码后写入的）
	if persisted, _ := st.GetConfig("token_secret"); persisted != "" {
		key = []byte(persisted)
		slog.Info("auth_loaded_persisted_key", "note", "从数据库加载持久化密钥")
	} else {
		// 生成临时密钥，token 有效期 2h
		randomKey := make([]byte, 32)
		rand.Read(randomKey)
		key = randomKey
		keyTemporary = true
		slog.Warn("auth_auto_generated_key",
			"note", "使用临时随机密钥（token 2h 有效），admin 修改密码后自动持久化",
			"random_secret", hex.EncodeToString(randomKey))
	}

	return &AuthHandler{
		cfg:          cfg,
		store:        st,
		jwtKey:       key,
		keyTemporary: keyTemporary,
	}
}

// LoginPage 登录页面
func (h *AuthHandler) LoginPage(c *gin.Context) {
	c.HTML(http.StatusOK, "login.html", gin.H{
		"page_title": h.cfg.Sys.Name,
	})
}

// RegisterPage 注册页面
func (h *AuthHandler) RegisterPage(c *gin.Context) {
	c.HTML(http.StatusOK, "register.html", gin.H{
		"page_title": h.cfg.Sys.Name,
	})
}

// Login 登录 API
func (h *AuthHandler) Login(c *gin.Context) {
	var req struct {
		UserName string `json:"user_name"`
		UserPwd  string `json:"user_pwd"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}

	user, err := h.store.GetUser(req.UserName)
	if err != nil {
		slog.Error("auth_login_db_error", "error", err, "username", req.UserName)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "系统错误"})
		return
	}
	if user == nil {
		slog.Warn("auth_login_user_not_found", "username", req.UserName)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "用户名或密码错误"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.UserPwd)); err != nil {
		slog.Warn("auth_login_wrong_password", "username", req.UserName)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "用户名或密码错误"})
		return
	}

	// admin 首次登录强制改密：2h 内未修改则初始密码失效
	if user.MustChangePwd {
		if expired := mustChangePwdExpired(user.CreatedAt); expired {
			slog.Warn("auth_must_change_pwd_expired", "username", user.UserName)
			c.JSON(http.StatusForbidden, gin.H{"error": "初始密码已过期（2小时内未修改），请联系管理员重置"})
			return
		}
	}

	// 临时密钥 token 有效期 2h，持久化密钥 7 天
	ttl := 7 * 24 * time.Hour
	if h.keyTemporary {
		ttl = 2 * time.Hour
	}

	// 生成简单的 token (HMAC-like, 适合内部系统)
	token := generateToken(user.ID, user.UserName, user.Role, h.jwtKey, ttl)

	// 设置 httpOnly cookie
	c.SetCookie("auth_token", token, int(ttl.Seconds()), "/", "", false, true)

	slog.Info("user_login", "user", user.UserName, "client_ip", c.ClientIP())

	c.JSON(http.StatusOK, gin.H{
		"status":          "ok",
		"token":           token,
		"must_change_pwd": user.MustChangePwd,
		"user": gin.H{
			"id":       user.ID,
			"username": user.UserName,
			"role":     user.Role,
		},
	})
}

// Logout 登出
func (h *AuthHandler) Logout(c *gin.Context) {
	token, _ := c.Cookie("auth_token")
	if u := parseToken(token, h.jwtKey); u != nil {
		slog.Info("user_logout", "user", u.UserName, "client_ip", c.ClientIP())
	}

	c.SetCookie("auth_token", "", -1, "/", "", false, true)
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// AuthMiddleware 认证中间件（Cookie）
func (h *AuthHandler) AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		token, err := c.Cookie("auth_token")
		if err != nil || token == "" {
			c.Redirect(http.StatusFound, "/login")
			c.Abort()
			return
		}

		user := parseToken(token, h.jwtKey)
		if user == nil {
			c.Redirect(http.StatusFound, "/login")
			c.Abort()
			return
		}

		c.Set("user", user)
		c.Next()
	}
}

// AdminOnlyMiddleware 管理员中间件
func (h *AuthHandler) AdminOnlyMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		user, _ := c.Get("user")
		if u, ok := user.(*model.User); !ok || u.Role != model.RoleAdmin {
			c.JSON(http.StatusForbidden, gin.H{"error": "需要管理员权限"})
			c.Abort()
			return
		}
		c.Next()
	}
}

// CookieAPIAuthMiddleware API 认证（Cookie）
func (h *AuthHandler) CookieAPIAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		token, err := c.Cookie("auth_token")
		if err != nil || token == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "请先登录"})
			c.Abort()
			return
		}

		user := parseToken(token, h.jwtKey)
		if user == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "登录已过期"})
			c.Abort()
			return
		}

		c.Set("user", user)
		c.Next()
	}
}

// BearerAuthMiddleware Bearer Token 认证（电子公文工作流调用）
// 已废弃，统一改用 APITokenMiddleware
func (h *AuthHandler) BearerAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" || len(authHeader) < 8 || authHeader[:7] != "Bearer " {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "缺少 Authorization header"})
			c.Abort()
			return
		}

		token := authHeader[7:]
		user := parseToken(token, h.jwtKey)
		if user == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "无效的 token"})
			c.Abort()
			return
		}

		c.Set("user", user)
		c.Next()
	}
}

// APITokenMiddleware 对外 API 认证中间件
// 支持两种认证方式：
//   1. X-Api-Key: {api_key} — 电子公文系统调用
//   2. Authorization: Bearer {token} — 兼容旧方式
// 管理员可配置开关和 token：api_auth_enabled=1 时校验，否则直接放行
func (h *AuthHandler) APITokenMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		enabled, _ := h.store.GetConfig("api_auth_enabled")
		if enabled != "1" {
			c.Next()
			return
		}

		expectedToken, _ := h.store.GetConfig("api_auth_token")
		if expectedToken == "" {
			c.Next()
			return
		}

		// 优先检查 X-Api-Key header
		apiKey := c.GetHeader("X-Api-Key")
		if apiKey != "" {
			if apiKey != expectedToken {
				c.JSON(http.StatusUnauthorized, model.NewErrorResponse(model.ErrCodeParamError, "无效的 X-Api-Key"))
				c.Abort()
				return
			}
			c.Next()
			return
		}

		// 回退 Bearer token
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" || len(authHeader) < 8 || authHeader[:7] != "Bearer " {
			c.JSON(http.StatusUnauthorized, model.NewErrorResponse(model.ErrCodeParamError, "缺少 X-Api-Key 或 Authorization header"))
			c.Abort()
			return
		}

		token := authHeader[7:]
		if token != expectedToken {
			c.JSON(http.StatusUnauthorized, model.NewErrorResponse(model.ErrCodeParamError, "无效的 token"))
			c.Abort()
			return
		}

		c.Next()
	}
}

// ============================================================
// Token 工具函数
// ============================================================

type tokenPayload struct {
	UserID   int64  `json:"uid"`
	UserName string `json:"uname"`
	Role     int    `json:"role"`
	Exp      int64  `json:"exp"`
	Salt     string `json:"salt"`
}

func generateToken(userID int64, userName string, role int, key []byte, ttl time.Duration) string {
	payload := tokenPayload{
		UserID:   userID,
		UserName: userName,
		Role:     role,
		Exp:      time.Now().Add(ttl).Unix(),
		Salt:     randomHex(8),
	}

	data, _ := json.Marshal(payload)
	encoded := hex.EncodeToString(data)

	// 追加简单 HMAC 签名
	mac := simpleHMAC(encoded, key)
	return encoded + "." + mac
}

func parseToken(token string, key []byte) *model.User {
	parts := splitN(token, ".", 2)
	if len(parts) != 2 {
		return nil
	}

	encoded, mac := parts[0], parts[1]
	if simpleHMAC(encoded, key) != mac {
		return nil
	}

	data, err := hex.DecodeString(encoded)
	if err != nil {
		return nil
	}

	var payload tokenPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil
	}

	if time.Now().Unix() > payload.Exp {
		return nil
	}

	return &model.User{
		ID:       payload.UserID,
		UserName: payload.UserName,
		Role:     payload.Role,
	}
}

// ============================================================
// 简易签名
// ============================================================

func simpleHMAC(data string, key []byte) string {
	combined := append([]byte(data), key...)
	var hash uint64 = 0
	for _, b := range combined {
		hash = hash*31 + uint64(b)
	}
	return fmt.Sprintf("%016x", hash)
}

func randomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func splitN(s, sep string, n int) []string {
	result := make([]string, 0, n)
	for i := 0; i < n-1; i++ {
		idx := indexOf(s, sep)
		if idx < 0 {
			result = append(result, s)
			return result
		}
		result = append(result, s[:idx])
		s = s[idx+len(sep):]
	}
	result = append(result, s)
	return result
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// GetUserInfo 从 context 获取当前用户
func GetUserInfo(c *gin.Context) (string, int) {
	user, exists := c.Get("user")
	if !exists {
		return "", -1
	}
	u, ok := user.(*model.User)
	if !ok {
		return "", -1
	}
	return u.UserName, u.Role
}

// GetTokenStr 从 cookie 获取 token
func GetTokenStr(c *gin.Context) string {
	token, _ := c.Cookie("auth_token")
	return token
}

// GenerateUserToken 为用户生成一个长期 API token
func (h *AuthHandler) GenerateUserToken(userID int64, userName string, role int) string {
	return generateToken(userID, userName, role, h.jwtKey, 7*24*time.Hour)
}

// persistKeyIfTemporary 临时密钥 → 持久化到 DB（admin 改密后调用）
func (h *AuthHandler) persistKeyIfTemporary() {
	if !h.keyTemporary {
		return
	}
	secret := hex.EncodeToString(h.jwtKey)
	if err := h.store.SetConfig("token_secret", secret, "admin 改密后持久化的 token 签名密钥"); err != nil {
		slog.Error("auth_persist_key_failed", "error", err)
		return
	}
	h.keyTemporary = false
	slog.Info("auth_key_persisted", "note", "admin 修改密码后，临时密钥已持久化到数据库")
}

// mustChangePwdExpired 判断强制改密是否过期（创建后 2h 内未改则过期）
func mustChangePwdExpired(createdAt string) bool {
	if createdAt == "" {
		return false
	}
	t, err := time.Parse("2006-01-02 15:04:05", createdAt)
	if err != nil {
		formats := []string{
			"2006-01-02T15:04:05Z",
			"2006-01-02T15:04:05-07:00",
			"2006-01-02 15:04:05-07:00",
		}
		for _, f := range formats {
			if t, err = time.Parse(f, createdAt); err == nil {
				break
			}
		}
		if err != nil {
			return true
		}
	}
	return time.Since(t) > 2*time.Hour
}