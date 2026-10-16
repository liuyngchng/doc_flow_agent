package handler

import (
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// hashPassword 生成密码哈希
func hashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(bytes), err
}

// comparePassword 验证密码
func comparePassword(hash, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}

// replaceAll 字符串替换（封装 strings.ReplaceAll）
func replaceAll(s, old, new string) string {
	return strings.ReplaceAll(s, old, new)
}