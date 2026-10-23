package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"doc_flow_agent/internal/model"

	"gopkg.in/yaml.v3"
)

// Load 从 YAML 文件加载配置；文件不存在时自动生成默认配置并写回
func Load(path string) (*model.Config, error) {
	cfg := defaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// 配置文件缺失：生成默认配置，方便用户在宿主机直接修改
			if err := writeDefaultConfig(path, cfg); err != nil {
				return nil, fmt.Errorf("生成默认配置文件失败: %w", err)
			}
			return cfg, nil
		}
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}

	applyDefaults(cfg)

	return cfg, nil
}

func defaultConfig() *model.Config {
	return &model.Config{
		Server: model.ServerConfig{
			Port:     20001,
			CertFile: "dist/cert.pem",
			KeyFile:  "dist/key.pem",
		},
		Sys: model.SysConfig{
			Name:                  "doc_flow_agent",
			BasePath:              "data",
			MaxConcurrentTasks:    5,
			MaxPendingTasks:       10,
			CallbackRetries:       3,
			CallbackRetryInterval: 2,
			RetentionDays:         30,
		},
		DocParser: model.DocParserConfig{
			URL: "",
		},
	}
}

func applyDefaults(cfg *model.Config) {
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 20001
	}
	if cfg.Sys.Name == "" {
		cfg.Sys.Name = "doc_flow_agent"
	}
	if cfg.Sys.BasePath == "" {
		cfg.Sys.BasePath = "data"
	}
	if cfg.Sys.MaxConcurrentTasks <= 0 {
		cfg.Sys.MaxConcurrentTasks = 5
	}
	if cfg.Sys.MaxPendingTasks <= 0 {
		cfg.Sys.MaxPendingTasks = 10
	}
	if cfg.Sys.CallbackRetries <= 0 {
		cfg.Sys.CallbackRetries = 3
	}
	if cfg.Sys.CallbackRetryInterval <= 0 {
		cfg.Sys.CallbackRetryInterval = 2
	}
	if cfg.Sys.RetentionDays <= 0 {
		cfg.Sys.RetentionDays = 30
	}
}

func writeDefaultConfig(path string, cfg *model.Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}