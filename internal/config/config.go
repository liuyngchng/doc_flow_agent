package config

import (
	"errors"
	"fmt"
	"os"

	"doc-ai-gateway/internal/model"

	"gopkg.in/yaml.v3"
)

// Load 从 YAML 文件加载配置；文件不存在时使用默认配置
func Load(path string) (*model.Config, error) {
	cfg := &model.Config{
		Server: model.ServerConfig{
			Port: 8080,
		},
		Sys: model.SysConfig{
			Name:     "DocAI Gateway",
			BasePath: "data",
		},
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// 配置文件缺失，使用默认配置
			return cfg, nil
		}
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}

	if cfg.Server.Port == 0 {
		cfg.Server.Port = 8080
	}
	if cfg.Sys.Name == "" {
		cfg.Sys.Name = "DocAI Gateway"
	}
	if cfg.Sys.BasePath == "" {
		cfg.Sys.BasePath = "data"
	}

	return cfg, nil
}