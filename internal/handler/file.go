package handler

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"doc-ai-gateway/internal/model"
	"doc-ai-gateway/internal/preprocess"
	"doc-ai-gateway/internal/store"

	"github.com/gin-gonic/gin"
)

// FileHandler 文件处理器
type FileHandler struct {
	cfg   *model.Config
	store store.MetaStore
}

// NewFileHandler 创建文件处理器
func NewFileHandler(cfg *model.Config, st store.MetaStore) *FileHandler {
	return &FileHandler{cfg: cfg, store: st}
}

// Upload 文件上传 (电子公文工作流调用)
func (h *FileHandler) Upload(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(400, gin.H{"error": "请上传文件"})
		return
	}

	bizID := c.PostForm("biz_id")
	customName := c.PostForm("file_name")
	originalName := file.Filename
	if customName != "" {
		originalName = customName
	}

	// 生成 file_id: F-YYYYMMDD-毫秒时间戳后4位
	fileID := fmt.Sprintf("F-%s%04d", time.Now().Format("20060102150405"), time.Now().Nanosecond()%10000)

	// 保存文件
	fileDir := filepath.Join(h.basePath(), "files", fileID)
	if err := os.MkdirAll(fileDir, 0755); err != nil {
		c.JSON(500, gin.H{"error": "创建目录失败"})
		return
	}

	localPath := filepath.Join(fileDir, originalName)
	if err := c.SaveUploadedFile(file, localPath); err != nil {
		c.JSON(500, gin.H{"error": "保存文件失败"})
		return
	}

	fileType := detectFileType(originalName)

	// 写入数据库
	record := &model.FileRecord{
		FileID:       fileID,
		OriginalName: originalName,
		FileType:     fileType,
		FilePath:     localPath,
		Status:       model.FileStatusProcessing,
		BizID:        bizID,
	}
	if err := h.store.CreateFileRecord(record); err != nil {
		c.JSON(500, gin.H{"error": "记录文件失败"})
		return
	}

	// 异步预处理
	go h.processFile(fileID, localPath)

	c.JSON(200, gin.H{
		"file_id":   fileID,
		"status":    model.FileStatusProcessing,
		"file_type": fileType,
		"message":   "文件已接收，预处理中",
	})
}

// Status 查询文件状态
func (h *FileHandler) Status(c *gin.Context) {
	fileID := c.Param("file_id")
	record, err := h.store.GetFileRecord(fileID)
	if err != nil || record == nil {
		c.JSON(404, gin.H{"error": "文件不存在"})
		return
	}

	c.JSON(200, gin.H{
		"file_id":        record.FileID,
		"original_name":  record.OriginalName,
		"status":         record.Status,
		"file_type":      record.FileType,
		"text_length":    record.TextLength,
		"ocr_confidence": record.OCRConfidence,
		"error_message":  record.ErrorMessage,
		"biz_id":         record.BizID,
		"created_at":     record.CreatedAt,
	})
}

// List 文件列表
func (h *FileHandler) List(c *gin.Context) {
	limit := 20
	offset := 0
	records, total, err := h.store.ListFileRecords(limit, offset)
	if err != nil {
		c.JSON(500, gin.H{"error": "查询失败"})
		return
	}

	c.JSON(200, gin.H{"data": records, "total": total})
}

// Delete 删除文件
func (h *FileHandler) Delete(c *gin.Context) {
	fileID := c.Param("file_id")
	record, err := h.store.GetFileRecord(fileID)
	if err != nil || record == nil {
		c.JSON(404, gin.H{"error": "文件不存在"})
		return
	}

	os.RemoveAll(filepath.Dir(record.FilePath))
	if err := h.store.DeleteFileRecord(fileID); err != nil {
		c.JSON(500, gin.H{"error": "删除失败"})
		return
	}

	c.JSON(200, gin.H{"status": "ok"})
}

func (h *FileHandler) processFile(fileID, localPath string) {
	result, err := preprocess.ExtractText(localPath)
	if err != nil {
		h.store.UpdateFileStatus(fileID, model.FileStatusFailed, err.Error())
		return
	}
	h.store.UpdateFileText(fileID, result.Text, len(result.Text), result.OCRConfidence)
}

func (h *FileHandler) basePath() string {
	if h.cfg.Sys.BasePath != "" {
		return h.cfg.Sys.BasePath
	}
	return "data"
}

func detectFileType(fileName string) string {
	switch filepath.Ext(fileName) {
	case ".docx":
		return "docx"
	case ".pdf":
		return "pdf"
	case ".xlsx", ".xls":
		return "xlsx"
	case ".jpg", ".jpeg", ".png", ".bmp", ".gif", ".tiff", ".tif":
		return "image"
	case ".ofd":
		return "ofd"
	default:
		return filepath.Ext(fileName)
	}
}