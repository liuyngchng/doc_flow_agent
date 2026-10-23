package main

import (
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"doc_flow_agent/internal/config"
	"doc_flow_agent/internal/handler"
	"doc_flow_agent/internal/logger"
		"doc_flow_agent/internal/store"

	"github.com/gin-gonic/gin"
)

//go:embed web
var webFS embed.FS

func main() {
	// 加载配置
	cfg, err := config.Load("data/cfg.yml")
	if err != nil {
		fmt.Fprintf(os.Stderr, "加载配置文件失败: %v\n", err)
		os.Exit(1)
	}

	// 初始化日志
	if err := logger.Init(cfg.Server.Debug); err != nil {
		fmt.Fprintf(os.Stderr, "初始化日志失败: %v\n", err)
		os.Exit(1)
	}

	// 初始化 SQLite 存储 — 放在 data/ 下，容器只需挂载这一个目录
	dbPath := "data/cfg.db"
	metaStore, err := store.NewSQLiteStore(dbPath)
	if err != nil {
		slog.Error("main_store_init_failed", "error", err)
		os.Exit(1)
	}
	defer metaStore.Close()

	// 初始化处理器
	h := handler.New(cfg, metaStore)

	// 启动异步 Worker Pool
	h.Worker.Start()
	defer h.Worker.Stop()

	// 启动定期数据清理（每天清理一次过期数据，保留 retention_days 天）
	go func() {
		retention := cfg.Sys.RetentionDays
		if retention <= 0 {
			retention = 30
		}
		doPrune := func() {
			paths, err := metaStore.PruneOldRecords(retention)
			if err != nil {
				slog.Error("main_prune_failed", "error", err)
				return
			}
			// 删除磁盘上的过期文件，文件已不存在时只打 warning 不报错
			for _, p := range paths {
				dir := filepath.Dir(p)
				if err := os.RemoveAll(dir); err != nil {
					if os.IsNotExist(err) {
						slog.Warn("main_prune_file_already_gone", "path", dir)
					} else {
						slog.Warn("main_prune_file_remove_failed", "path", dir, "error", err)
					}
				}
			}
		}
		// 启动时先清理一次
		doPrune()
		// 之后每 24h 清理一次
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			doPrune()
		}
	}()

	// 设置 Gin
	if !cfg.Server.Debug {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	r.Use(logger.GinLogger(), gin.Recovery())
	r.Use(handler.SecurityHeaders())

	// 加载 HTML 模板
	tmpl, err := template.ParseFS(webFS, "web/templates/*")
	if err != nil {
		slog.Error("main_template_load_failed", "error", err)
		os.Exit(1)
	}
	r.SetHTMLTemplate(tmpl)

	// 静态文件
	staticFS, err := fs.Sub(webFS, "web/static")
	if err != nil {
		slog.Error("main_static_files_load_failed", "error", err)
		os.Exit(1)
	}
	r.StaticFS("/static", http.FS(staticFS))

	// ============================================================
	// 公开路由
	// ============================================================
	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})
	r.GET("/login", h.Auth.LoginPage)
	r.GET("/register", h.Auth.RegisterPage)
	r.POST("/api/login", h.Auth.Login)
	r.POST("/api/logout", h.Auth.Logout)
	r.POST("/api/register", h.User.Register)

	// 根路径重定向到管理后台
	r.GET("/", func(c *gin.Context) {
		c.Redirect(http.StatusFound, "/admin")
	})

	// ============================================================
	// 管理后台（Cookie 认证）
	// ============================================================
	adminPage := r.Group("/admin")
	adminPage.Use(h.Auth.AuthMiddleware())
	{
		adminPage.GET("/", h.Page.AdminIndex)
	}

	// ============================================================
	// /api/inner/ — 管理后台 API（Cookie 认证）
	// ============================================================
	v1 := r.Group("/api/inner")
	v1.Use(h.Auth.CookieAPIAuthMiddleware())
	{
		// 任务追踪 — 所有登录用户（含只读用户）可查看
		v1.GET("/task-trace", h.CallLog.List)
		v1.GET("/task-trace/:task_id", h.CallLog.Get)

		// 以下为管理员专属
		admin := v1.Group("")
		admin.Use(h.Auth.AdminOnlyMiddleware())
		{
			// 配置
			admin.GET("/config", h.Config.GetConfig)
			admin.PUT("/config", h.Config.UpdateConfig)

			// LLM 模型配置
			admin.GET("/llm-configs", h.LLMConfig.List)
			admin.POST("/llm-configs", h.LLMConfig.Create)
			admin.PUT("/llm-configs/:id", h.LLMConfig.Update)
			admin.DELETE("/llm-configs/:id", h.LLMConfig.Delete)
			admin.PUT("/llm-configs/:id/default", h.LLMConfig.SetDefault)

			// 接口管理
			admin.GET("/interfaces", h.Interface.List)
			admin.GET("/interfaces/:id", h.Interface.Get)
			admin.POST("/interfaces", h.Interface.Create)
			admin.PUT("/interfaces/:id", h.Interface.Update)
			admin.DELETE("/interfaces/:id", h.Interface.Delete)
			admin.POST("/interfaces/:id/test", h.Interface.Test)

			// 文件管理
			admin.GET("/files", h.File.List)
			admin.DELETE("/files/:file_id", h.File.Delete)

			// 用户管理
			admin.GET("/users", h.User.ListUsers)
			admin.POST("/users", h.User.CreateUser)
			admin.DELETE("/users/:name", h.User.DeleteUser)

			// 审稿模拟
			admin.POST("/proofread/submit", h.Proofread.Submit)
			admin.GET("/proofread/query/:task_id", h.Proofread.Query)
			admin.GET("/proofread/state", h.Proofread.State)
		}
	}

	// 用户自助 API（改密码 — 所有登录用户）
	v1User := v1.Group("/user")
	{
		v1User.PUT("/password", h.User.ChangePassword)
	}

	// ============================================================
	// 对外接口（电子公文工作流调用）
	// ============================================================
	// 文件上传（不需要 访问令牌，直接用 multipart）
	r.POST("/api/files/upload", h.File.Upload)
	r.GET("/api/files/status/:file_id", h.File.Status)

	// 电子公文工作流调用接口（支持 X-Api-Key 或 Bearer token）
	r.POST("/api/v1/*interfacePath", h.Auth.APITokenMiddleware(), handler.CallLogMiddleware(metaStore), h.Call.Call)

	// 异步任务管理（查询进行中任务 / 取消排队中任务 / 查看任务追踪）
	r.GET("/api/v1/tasks", h.Auth.APITokenMiddleware(), h.AsyncTask.ListTasks)
	r.DELETE("/api/v1/tasks/:task_id", h.Auth.APITokenMiddleware(), h.AsyncTask.DeleteTask)
	r.GET("/api/v1/task/trace", h.Auth.APITokenMiddleware(), h.AsyncTask.Trace)

	// 优雅退出
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		slog.Info("main_shutting_down")
		h.Worker.Stop()
		metaStore.Close()
		os.Exit(0)
	}()

	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	if cfg.Server.CertFile != "" && cfg.Server.KeyFile != "" {
		slog.Info("main_server_started", "addr", addr, "url", fmt.Sprintf("https://localhost%s", addr), "cert", cfg.Server.CertFile, "key", cfg.Server.KeyFile)
		if err := r.RunTLS(addr, cfg.Server.CertFile, cfg.Server.KeyFile); err != nil {
			slog.Error("main_server_start_failed", "error", err)
			os.Exit(1)
		}
		return
	}

	slog.Info("main_server_started", "addr", addr, "url", fmt.Sprintf("http://localhost%s", addr))
	if err := r.Run(addr); err != nil {
		slog.Error("main_server_start_failed", "error", err)
		os.Exit(1)
	}
}