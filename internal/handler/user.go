package handler

import (
	"log/slog"
	"net/http"

	"doc_flow_agent/internal/model"
	"doc_flow_agent/internal/store"

	"github.com/gin-gonic/gin"
)

// UserHandler 用户管理处理器
type UserHandler struct {
	store store.MetaStore
	auth  *AuthHandler
}

// NewUserHandler 创建用户处理器
func NewUserHandler(st store.MetaStore, auth *AuthHandler) *UserHandler {
	return &UserHandler{store: st, auth: auth}
}

// ListUsers 列出用户
func (h *UserHandler) ListUsers(c *gin.Context) {
	users, err := h.store.ListUsers()
	if err != nil {
		c.JSON(500, gin.H{"error": "查询失败"})
		return
	}
	if users == nil {
		users = []model.User{}
	}
	c.JSON(200, gin.H{"data": users})
}

// CreateUser 创建用户
func (h *UserHandler) CreateUser(c *gin.Context) {
	var req struct {
		UserName string `json:"user_name"`
		UserPwd  string `json:"user_pwd"`
		Role     int    `json:"role"`
		Note     string `json:"note"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.UserName == "" || req.UserPwd == "" {
		c.JSON(400, gin.H{"error": "用户名和密码不能为空"})
		return
	}

	hash, err := hashPassword(req.UserPwd)
	if err != nil {
		c.JSON(500, gin.H{"error": "加密失败"})
		return
	}

	user := &model.User{
		UserName:     req.UserName,
		PasswordHash: hash,
		Role:         req.Role,
		Note:         req.Note,
	}
	if err := h.store.CreateUser(user); err != nil {
		c.JSON(500, gin.H{"error": "创建失败: " + err.Error()})
		return
	}
	slog.Info("admin_create_user", "user", userName(c), "target", req.UserName, "role", req.Role)
	c.JSON(200, gin.H{"status": "ok"})
}

// DeleteUser 删除用户
func (h *UserHandler) DeleteUser(c *gin.Context) {
	targetName := c.Param("name")
	if targetName == "admin" {
		c.JSON(400, gin.H{"error": "不能删除 admin 用户"})
		return
	}
	if err := h.store.DeleteUser(targetName); err != nil {
		c.JSON(500, gin.H{"error": "删除失败"})
		return
	}
	slog.Info("admin_delete_user", "user", userName(c), "target", targetName)
	c.JSON(200, gin.H{"status": "ok"})
}

// ChangePassword 修改密码
func (h *UserHandler) ChangePassword(c *gin.Context) {
	var req struct {
		OldPwd string `json:"old_pwd"`
		NewPwd string `json:"new_pwd"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.NewPwd == "" {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}

	userName, _ := GetUserInfo(c)
	user, err := h.store.GetUser(userName)
	if err != nil || user == nil {
		c.JSON(404, gin.H{"error": "用户不存在"})
		return
	}

	// 验证旧密码（跳过 admin 首次修改的场景）
	if req.OldPwd != "" {
		if err := comparePassword(user.PasswordHash, req.OldPwd); err != nil {
			c.JSON(400, gin.H{"error": "旧密码错误"})
			return
		}
	}

	hash, err := hashPassword(req.NewPwd)
	if err != nil {
		c.JSON(500, gin.H{"error": "加密失败"})
		return
	}

	if err := h.store.UpdatePassword(userName, hash); err != nil {
		c.JSON(500, gin.H{"error": "修改失败"})
		return
	}

	// 清除强制改密标记
	_ = h.store.ClearMustChangePwd(userName)

	// 若当前是临时密钥，admin 改密后持久化密钥到 DB，保证重启后 token 仍有效
	h.auth.persistKeyIfTemporary()

	c.JSON(200, gin.H{"status": "ok"})
}

// Register 用户注册
func (h *UserHandler) Register(c *gin.Context) {
	var req struct {
		UserName string `json:"user_name"`
		UserPwd  string `json:"user_pwd"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.UserName == "" || req.UserPwd == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "用户名和密码不能为空"})
		return
	}

	// 检查用户名是否已存在
	existing, _ := h.store.GetUser(req.UserName)
	if existing != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "用户名已存在"})
		return
	}

	hash, err := hashPassword(req.UserPwd)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "加密失败"})
		return
	}

	user := &model.User{
		UserName:     req.UserName,
		PasswordHash: hash,
		Role:         model.RoleNormal,
	}
	if err := h.store.CreateUser(user); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "注册失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}