package preprocess

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

func extractXlsxText(filePath string) (*Result, error) {
	r, err := zip.OpenReader(filePath)
	if err != nil {
		return nil, fmt.Errorf("打开 XLSX 失败: %w", err)
	}
	defer r.Close()

	// 1. 读取共享字符串表
	var sharedStrings []string
	for _, f := range r.File {
		if f.Name == "xl/sharedStrings.xml" {
			rc, openErr := f.Open()
			if openErr != nil {
				return nil, fmt.Errorf("读取 sharedStrings.xml 失败: %w", openErr)
			}

			type Si struct {
				T string `xml:"t"`
			}
			type SST struct {
				Si []Si `xml:"si"`
			}

			data, readErr := io.ReadAll(rc)
			rc.Close()
			if readErr != nil {
				return nil, fmt.Errorf("读取共享字符串数据失败: %w", readErr)
			}

			var sst SST
			if xml.Unmarshal(data, &sst) == nil {
				for _, si := range sst.Si {
					sharedStrings = append(sharedStrings, si.T)
				}
			}
			break
		}
	}

	// 2. 读取第一个 sheet
	var sheetFile *zip.File
	for _, f := range r.File {
		if strings.HasPrefix(f.Name, "xl/worksheets/sheet") && strings.HasSuffix(f.Name, ".xml") {
			sheetFile = f
			break
		}
	}
	if sheetFile == nil {
		return nil, fmt.Errorf("无效的 XLSX 文件: 缺少 sheet 数据")
	}

	rc, err := sheetFile.Open()
	if err != nil {
		return nil, fmt.Errorf("读取 sheet 失败: %w", err)
	}
	defer rc.Close()

	type Cell struct {
		T string `xml:"t,attr"`
		V string `xml:"v"`
		Is struct {
			T string `xml:"t"`
		} `xml:"is"`
	}
	type Row struct {
		Cells []Cell `xml:"c"`
	}
	type SheetData struct {
		Rows []Row `xml:"row"`
	}
	type Worksheet struct {
		SheetData SheetData `xml:"sheetData"`
	}

	data, readErr := io.ReadAll(rc)
	if readErr != nil {
		return nil, fmt.Errorf("读取 sheet 数据失败: %w", readErr)
	}

	var ws Worksheet
	if err := xml.Unmarshal(data, &ws); err != nil {
		return nil, fmt.Errorf("解析 XLSX XML 失败: %w", err)
	}

	var rows []string
	for _, row := range ws.SheetData.Rows {
		var cells []string
		for _, c := range row.Cells {
			val := ""
			switch c.T {
			case "s":
				idx := 0
				fmt.Sscanf(c.V, "%d", &idx)
				if idx >= 0 && idx < len(sharedStrings) {
					val = sharedStrings[idx]
				}
			case "inlineStr":
				val = c.Is.T
			default:
				val = c.V
			}
			if val != "" {
				cells = append(cells, val)
			}
		}
		if len(cells) > 0 {
			rows = append(rows, strings.Join(cells, "\t"))
		}
	}

	result := strings.Join(rows, "\n")
	if result == "" {
		return nil, fmt.Errorf("XLSX 文件无内容")
	}

	return &Result{
		Text:     result,
		FileType: "xlsx",
	}, nil
}