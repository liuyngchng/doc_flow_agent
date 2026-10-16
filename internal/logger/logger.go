package logger

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	rotatelogs "github.com/lestrrat-go/file-rotatelogs"
)

// SourceWidth 源码位置显示宽度（右对齐用）
const SourceWidth = 24

// modulePrefix 模块名前缀，从完整函数名中剥离
const modulePrefix = "doc-ai-gateway/"

// customHandler 自定义日志格式
// 格式: 2026-10-15 14:03:41.238 WARN  [handler/auth.go:123] auth_auto_generated_key note=xxx
type customHandler struct {
	w      io.Writer
	level  slog.Leveler
	attrs  []slog.Attr
	groups []string
}

func (h *customHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level.Level()
}

func (h *customHandler) Handle(_ context.Context, r slog.Record) error {
	// 1. 时间
	buf := []byte(r.Time.Format("2006-01-02 15:04:05.000"))

	// 2. 级别（补齐到 5 字符）
	buf = append(buf, ' ')
	buf = append(buf, padLevel(r.Level.String())...)

	// 3. 源码位置 [文件:行号]
	if r.PC != 0 {
		if src := sourceFromPC(r.PC); src != nil {
			loc := sourceLocation(src)
			buf = append(buf, ' ')
			buf = append(buf, '[')
			if len(loc) < SourceWidth {
				buf = append(buf, bytes.Repeat([]byte{' '}, SourceWidth-len(loc))...)
			}
			buf = append(buf, loc...)
			buf = append(buf, ']')
		}
	}

	// 4. 消息
	buf = append(buf, ' ')
	buf = append(buf, r.Message...)

	// 5. 属性 (key=value)
	r.Attrs(func(a slog.Attr) bool {
		buf = append(buf, ' ')
		buf = append(buf, a.Key...)
		buf = append(buf, '=')
		buf = append(buf, ' ')
		buf = append(buf, a.Value.String()...)
		return true
	})

	buf = append(buf, '\n')

	_, err := h.w.Write(buf)
	return err
}

func (h *customHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &customHandler{
		w:     h.w,
		level: h.level,
		attrs: append(h.attrs, attrs...),
	}
}

func (h *customHandler) WithGroup(name string) slog.Handler {
	return &customHandler{
		w:      h.w,
		level:  h.level,
		attrs:  h.attrs,
		groups: append(h.groups, name),
	}
}

// sourceFromPC 从 PC 解析 slog.Source 信息
func sourceFromPC(pc uintptr) *slog.Source {
	fs := runtime.CallersFrames([]uintptr{pc})
	f, _ := fs.Next()
	if f.File == "" {
		return nil
	}
	return &slog.Source{
		Function: f.Function,
		File:     f.File,
		Line:     f.Line,
	}
}

// sourceLocation 生成 "internal/handler/auth.go:123" 格式
func sourceLocation(src *slog.Source) string {
	pkg := strings.TrimPrefix(src.Function, modulePrefix)

	if i := strings.Index(pkg, "("); i > 0 {
		pkg = pkg[:i]
	} else if i := strings.LastIndex(pkg, "."); i > 0 {
		pkg = pkg[:i]
	}
	pkg = strings.TrimSuffix(pkg, ".")

	name := filepath.Base(src.File)
	name = name[:len(name)-len(filepath.Ext(name))]
	return abbreviatePath(pkg) + "/" + name + ":" + strconv.Itoa(src.Line)
}

// abbreviatePath 将包路径每段缩写为首字母
func abbreviatePath(pkg string) string {
	segs := strings.Split(pkg, "/")
	for i, seg := range segs {
		if seg == "" {
			continue
		}
		for _, r := range seg {
			segs[i] = string(r)
			break
		}
	}
	return strings.Join(segs, "/")
}

func padLevel(level string) string {
	if len(level) < 5 {
		return level + strings.Repeat(" ", 5-len(level))
	}
	return level
}

// Init 初始化日志系统
// 始终同时输出到控制台和按小时滚动的日志文件
// Windows GUI 模式下无控制台，日志仅看文件
func Init(debug bool) error {
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}

	// 确保日志目录存在（rotatelogs 不会自动创建）
	if err := os.MkdirAll("data/logs", 0755); err != nil {
		return err
	}

	fileWriter, err := rotatelogs.New(
		"data/logs/app.%Y%m%d%H.log",
		rotatelogs.WithMaxAge(7*24*time.Hour),
		rotatelogs.WithRotationTime(time.Hour),
	)
	if err != nil {
		return err
	}

	multiWriter := io.MultiWriter(os.Stdout, fileWriter)

	handler := &customHandler{w: multiWriter, level: level}
	slog.SetDefault(slog.New(handler))

	// Gin 日志也重定向到统一 writer
	gin.DefaultWriter = multiWriter
	gin.DefaultErrorWriter = multiWriter

	return nil
}

// GinLogger Gin 请求日志中间件
func GinLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery

		c.Next()

		statusCode := c.Writer.Status()
		if statusCode < 400 {
			return
		}

		latency := time.Since(start)
		method := c.Request.Method
		clientIP := c.ClientIP()

		level := slog.LevelWarn
		if statusCode >= 500 {
			level = slog.LevelError
		}

		slog.LogAttrs(c.Request.Context(), level,
			"gin",
			slog.Int("status", statusCode),
			slog.String("method", method),
			slog.String("path", path),
			slog.String("query", query),
			slog.String("ip", clientIP),
			slog.Duration("latency", latency),
			slog.Int("size", c.Writer.Size()),
		)
	}
}