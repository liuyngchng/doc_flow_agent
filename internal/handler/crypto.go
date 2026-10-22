package handler

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

// userName 从上下文获取当前登录用户名，未登录时返回空字符串
func userName(c *gin.Context) string {
	name, _ := GetUserInfo(c)
	return name
}

// hashPassword 生成密码哈希
func hashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(bytes), err
}

// comparePassword 验证密码
func comparePassword(hash, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}

// validatePasswordStrength 校验密码强度：至少 8 位，且同时包含字母和数字
func validatePasswordStrength(password string) error {
	if utf8.RuneCountInString(password) < 8 {
		return fmt.Errorf("密码长度至少 8 位")
	}
	var hasLetter, hasDigit bool
	for _, r := range password {
		switch {
		case unicode.IsLetter(r):
			hasLetter = true
		case unicode.IsDigit(r):
			hasDigit = true
		}
	}
	if !hasLetter {
		return fmt.Errorf("密码必须包含字母")
	}
	if !hasDigit {
		return fmt.Errorf("密码必须包含数字")
	}
	return nil
}

// replaceAll 字符串替换（封装 strings.ReplaceAll）
func replaceAll(s, old, new string) string {
	return strings.ReplaceAll(s, old, new)
}

// boolStr 布尔值转字符串，true -> "1"，false -> "0"
func boolStr(b bool) string {
	if b {
		return "1"
	}
	return "0"
}