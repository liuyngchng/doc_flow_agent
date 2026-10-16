package preprocess

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/ledongthuc/pdf"
)

func extractPdfText(filePath string) (*Result, error) {
	f, r, err := pdf.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("打开 PDF 失败: %w", err)
	}
	defer f.Close()

	var buf bytes.Buffer
	numPages := r.NumPage()
	for i := 1; i <= numPages; i++ {
		page := r.Page(i)
		if page.V.IsNull() {
			continue
		}
		text, err := page.GetPlainText(nil)
		if err != nil {
			continue
		}
		buf.WriteString(text)
		buf.WriteString("\n")
	}

	result := strings.TrimSpace(buf.String())
	isScanned := false
	if result == "" {
		isScanned = true
		result = "[扫描件 - 需要 OCR 识别]"
	}

	return &Result{
		Text:      result,
		FileType:  "pdf",
		PageCount: numPages,
		IsScanned: isScanned,
	}, nil
}