package preprocess

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

func extractDocxText(filePath string) (*Result, error) {
	r, err := zip.OpenReader(filePath)
	if err != nil {
		return nil, fmt.Errorf("打开 DOCX 失败: %w", err)
	}
	defer r.Close()

	var documentXML *zip.File
	for _, f := range r.File {
		if f.Name == "word/document.xml" {
			documentXML = f
			break
		}
	}
	if documentXML == nil {
		return nil, fmt.Errorf("无效的 DOCX 文件: 缺少 word/document.xml")
	}

	rc, err := documentXML.Open()
	if err != nil {
		return nil, fmt.Errorf("读取 document.xml 失败: %w", err)
	}
	defer rc.Close()

	type Text struct {
		Content string `xml:",chardata"`
	}
	type Run struct {
		Text []Text `xml:"t"`
	}
	type Paragraph struct {
		Runs []Run `xml:"r"`
	}
	type Body struct {
		Paragraphs []Paragraph `xml:"p"`
	}
	type Document struct {
		Body Body `xml:"body"`
	}

	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, fmt.Errorf("读取 XML 数据失败: %w", err)
	}

	var doc Document
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("解析 DOCX XML 失败: %w", err)
	}

	var paragraphs []string
	for _, p := range doc.Body.Paragraphs {
		var line string
		for _, r := range p.Runs {
			for _, t := range r.Text {
				line += t.Content
			}
		}
		if strings.TrimSpace(line) != "" {
			paragraphs = append(paragraphs, line)
		}
	}

	result := strings.Join(paragraphs, "\n")
	if result == "" {
		return nil, fmt.Errorf("DOCX 文件无文本内容")
	}

	return &Result{
		Text:     result,
		FileType: "docx",
	}, nil
}