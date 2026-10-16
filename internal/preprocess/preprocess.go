package preprocess

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Result 预处理结果
type Result struct {
	Text          string  `json:"text"`
	FileType      string  `json:"file_type"`
	PageCount     int     `json:"page_count"`
	OCRConfidence float64 `json:"ocr_confidence"`
	IsScanned     bool    `json:"is_scanned"`
}

// ExtractText 根据文件路径提取文本
func ExtractText(filePath string) (*Result, error) {
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".docx":
		return extractDocxText(filePath)
	case ".pdf":
		return extractPdfText(filePath)
	case ".xlsx":
		return extractXlsxText(filePath)
	default:
		return nil, fmt.Errorf("不支持的文件格式: %s", ext)
	}
}