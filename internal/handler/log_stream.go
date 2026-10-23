package handler

import (
	"bufio"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// LogStream GET /api/inner/logs/stream?task_id=xxx
// SSE 实时推送匹配 task_id 的日志行，类似 tail -f app.log | grep <task_id>
func LogStream(c *gin.Context) {
	taskID := strings.TrimSpace(c.Query("task_id"))
	if taskID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 task_id 参数"})
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	clientGone := c.Request.Context().Done()
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		slog.Error("log_stream_no_flusher")
		return
	}

	// 定位当前日志文件
	logFile := currentLogFile()
	f, err := os.Open(logFile)
	if err != nil {
		sendSSE(c.Writer, flusher, "error", "无法打开日志文件: "+err.Error())
		return
	}
	defer f.Close()

	// 不从头读，先 seek 到末尾（只推送新日志）
	// 但如果文件刚轮转，可能已经有不含该 task_id 的历史行，跳过即可
	stat, _ := f.Stat()
	pos := stat.Size()
	if pos > 0 {
		f.Seek(pos, io.SeekStart)
	}

	reader := bufio.NewReader(f)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	sendSSE(c.Writer, flusher, "ready", fmt.Sprintf("开始监听 task_id=%s 的日志...", taskID))

	for {
		select {
		case <-clientGone:
			return
		case <-ticker.C:
			// 检查文件是否轮转
			newFile := currentLogFile()
			if newFile != logFile {
				f.Close()
				logFile = newFile
				f, _ = os.Open(logFile)
				reader = bufio.NewReader(f)
			}

			// 读取新增行
			for {
				line, err := reader.ReadString('\n')
				if err != nil {
					break
				}
				// 过滤：包含 task_id 的行
				if strings.Contains(line, taskID) {
					sendSSE(c.Writer, flusher, "log", strings.TrimRight(line, "\n"))
				}
			}
		}
	}
}

// currentLogFile 返回当前小时的日志文件路径
func currentLogFile() string {
	return filepath.Join("data", "logs", "app."+time.Now().Format("2006010215")+".log")
}

func sendSSE(w io.Writer, flusher http.Flusher, event, data string) {
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
	flusher.Flush()
}