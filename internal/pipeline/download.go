package pipeline

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"doc_flow_agent/internal/model"
	"doc_flow_agent/internal/preprocess"
)

var dlClient = &http.Client{Timeout: 60 * time.Second}

// ============================================================
// download 步骤：提取 file_url → 下载 → 提取文本
// ============================================================

func (ctx *TaskContext) runDownload() error {
	items := extractFileURLs(ctx.RawBody)
	if len(items) == 0 {
		return nil
	}

	result := &model.DownloadResult{}
	tmpDir := filepath.Join(ctx.basePath(), "tmp")
	os.MkdirAll(tmpDir, 0755)

	sealSeen := make(map[string]bool)

	for i, item := range items {
		slog.Info("pipeline_download", "task_id", ctx.TaskID, "url", item.URL, "file_type", item.FileType)

		itemStart := time.Now()
		df := model.DownloadedFile{
			URL:      item.URL,
			FileType: item.FileType,
			FileName: item.FileName,
		}

		// 下载
		resp, err := dlClient.Get(item.URL)
		if err != nil {
			slog.Warn("pipeline_download_failed", "task_id", ctx.TaskID, "url", item.URL, "error", err)
			df.Status = "download_failed"
			df.ErrorMsg = err.Error()
			df.DurationMs = time.Since(itemStart).Milliseconds()
			result.FlatItems = append(result.FlatItems, df)
			continue
		}

		fileBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil || resp.StatusCode != http.StatusOK {
			slog.Warn("pipeline_download_failed", "task_id", ctx.TaskID, "url", item.URL, "status", resp.StatusCode, "error", err)
			df.Status = "download_failed"
			if err != nil {
				df.ErrorMsg = err.Error()
			} else {
				df.ErrorMsg = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(fileBody[:minInt(len(fileBody), 200)]))
			}
			df.DurationMs = time.Since(itemStart).Milliseconds()
			result.FlatItems = append(result.FlatItems, df)
			continue
		}

		// 确定扩展名
		ext := detectExt(item.URL, resp.Header.Get("Content-Type"))

		// 写入临时文件
		tmpFile := filepath.Join(tmpDir, fmt.Sprintf("dl_%d_%d%s", time.Now().UnixNano(), i, ext))
		if err := os.WriteFile(tmpFile, fileBody, 0644); err != nil {
			slog.Warn("pipeline_tmp_write_failed", "task_id", ctx.TaskID, "url", item.URL, "error", err)
			df.Status = "download_failed"
			df.ErrorMsg = err.Error()
			df.DurationMs = time.Since(itemStart).Milliseconds()
			result.FlatItems = append(result.FlatItems, df)
			continue
		}
		defer os.Remove(tmpFile)

		df.LocalPath = tmpFile

		// 提取文本及印章信息
		pd := ctx.extractFileText(tmpFile)
		if pd == nil || pd.Text == "" {
			slog.Warn("pipeline_extract_empty", "task_id", ctx.TaskID, "url", item.URL)
			df.Status = "extract_failed"
			df.DurationMs = time.Since(itemStart).Milliseconds()
			result.FlatItems = append(result.FlatItems, df)
			continue
		}
		df.Text = pd.Text
		df.HasSeal = pd.HasSeal
		df.SealTexts = pd.SealText
		df.Status = "ok"
		df.DurationMs = time.Since(itemStart).Milliseconds()

		// 汇总印章
		for _, s := range pd.SealText {
			if s != "" && !sealSeen[s] {
				sealSeen[s] = true
				result.SealTexts = append(result.SealTexts, s)
			}
		}

		// 构造文本块
		block := pd.Text
		if item.FileName != "" {
			block = fmt.Sprintf("【%s】\n%s", item.FileName, block)
		}
		if len(pd.SealText) > 0 {
			block = block + "\n（印章文字：" + strings.Join(pd.SealText, "、") + "）"
		}

		switch item.FileType {
		case "main_body":
			result.MainBody = append(result.MainBody, block)
		case "attachment":
			result.Attachments = append(result.Attachments, block)
		case "doc_basis":
			result.DocBasis = append(result.DocBasis, block)
		default:
			result.MainBody = append(result.MainBody, block)
		}

		result.FlatItems = append(result.FlatItems, df)
		slog.Info("pipeline_download_done", "task_id", ctx.TaskID, "url", item.URL, "file_type", item.FileType, "text_len", len(pd.Text), "has_seal", pd.HasSeal)
	}

	ctx.DownloadResult = result
	return nil
}

// runExtract 在 download 中已完成（download 内部即下载+提取），此步骤为空
func (ctx *TaskContext) runExtract() error {
	// 下载步骤中已同步完成提取，无需额外操作
	// 保留此方法供可能的覆盖场景（如纯 OCR 模式、仅下载不提取等）
	return nil
}

// extractFileText 提取文件文本
func (ctx *TaskContext) extractFileText(localPath string) *model.ParsedDocument {
	if ctx.Cfg.DocParser.URL != "" {
		if pd, err := ctx.extractTextRemote(localPath); err == nil {
			return pd
		}
		slog.Warn("pipeline_remote_parse_failed", "path", localPath)
	}

	result, err := preprocess.ExtractText(localPath)
	if err != nil {
		slog.Warn("pipeline_local_parse_failed", "path", localPath, "error", err)
		return nil
	}
	return &model.ParsedDocument{
		Text:          result.Text,
		OCRConfidence: result.OCRConfidence,
		HasSeal:       false,
		SealText:      []string{},
	}
}

// extractTextRemote 调用外部文档解析服务（multipart/form-data）
func (ctx *TaskContext) extractTextRemote(localPath string) (*model.ParsedDocument, error) {
	f, err := os.Open(localPath)
	if err != nil {
		return nil, fmt.Errorf("打开文件失败: %w", err)
	}
	defer f.Close()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filepath.Base(localPath))
	if err != nil {
		return nil, fmt.Errorf("创建 form 字段失败: %w", err)
	}
	if _, err := io.Copy(part, f); err != nil {
		return nil, fmt.Errorf("写入文件数据失败: %w", err)
	}
	writer.Close()

	req, err := http.NewRequest(http.MethodPost, ctx.Cfg.DocParser.URL, &body)
	if err != nil {
		return nil, fmt.Errorf("创建解析请求失败: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if ctx.Cfg.DocParser.Token != "" {
		req.Header.Set("Authorization", "Bearer "+ctx.Cfg.DocParser.Token)
	}

	resp, err := docParserClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("调用文档解析服务失败: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取解析响应失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("文档解析服务返回错误状态码 %d: %s", resp.StatusCode, string(respBody))
	}

	pd := &model.ParsedDocument{}
	if err := json.Unmarshal(respBody, pd); err != nil {
		return nil, fmt.Errorf("解析 JSON 响应失败: %w", err)
	}
	return pd, nil
}

// ============================================================
// 辅助函数
// ============================================================

// extractFileURLs 递归从 JSON 中提取所有 file_url
func extractFileURLs(v interface{}) []model.FileURLItem {
	var items []model.FileURLItem
	seen := make(map[string]bool)

	var walk func(v interface{})
	walk = func(v interface{}) {
		switch val := v.(type) {
		case map[string]interface{}:
			if url, ok := val["file_url"].(string); ok && url != "" && !seen[url] {
				seen[url] = true
				item := model.FileURLItem{URL: url}
				if ft, ok := val["file_type"].(string); ok {
					item.FileType = ft
				}
				if fn, ok := val["file_name"].(string); ok {
					item.FileName = fn
				}
				items = append(items, item)
			}
			for k, vv := range val {
				if k == "file_url" {
					continue
				}
				walk(vv)
			}
		case []interface{}:
			for _, item := range val {
				walk(item)
			}
		}
	}
	walk(v)
	return items
}

// detectExt 从 URL 或 Content-Type 推断文件扩展名
func detectExt(url, contentType string) string {
	if idx := strings.Index(url, "?"); idx >= 0 {
		url = url[:idx]
	}
	ext := strings.ToLower(filepath.Ext(url))
	switch ext {
	case ".pdf", ".docx", ".xlsx", ".ofd", ".doc", ".xls", ".png", ".jpg", ".jpeg":
		return ext
	}
	switch {
	case strings.Contains(contentType, "pdf"):
		return ".pdf"
	case strings.Contains(contentType, "word") || strings.Contains(contentType, "docx"):
		return ".docx"
	case strings.Contains(contentType, "excel") || strings.Contains(contentType, "xlsx") || strings.Contains(contentType, "spreadsheet"):
		return ".xlsx"
	case strings.Contains(contentType, "image"):
		return ".png"
	default:
		return ".pdf"
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (ctx *TaskContext) basePath() string {
	if ctx.Cfg.Sys.BasePath != "" {
		return ctx.Cfg.Sys.BasePath
	}
	return "data"
}