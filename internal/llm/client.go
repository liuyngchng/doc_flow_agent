package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"doc-ai-gateway/internal/model"
)

var llmHTTPClient = &http.Client{
	Transport: &http.Transport{
		Proxy: nil,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 20,
		IdleConnTimeout:     90 * time.Second,
	},
	Timeout: 180 * time.Second,
}

// Client LLM 客户端 (OpenAI 兼容接口)
type Client struct {
	BaseURL   string
	APIKey    string
	ModelName string
}

// New 创建 LLM 客户端
func New(baseURL, apiKey, modelName string) *Client {
	return &Client{
		BaseURL:   strings.TrimRight(baseURL, "/"),
		APIKey:    apiKey,
		ModelName: modelName,
	}
}

// Chat 非流式聊天
func (c *Client) Chat(systemPrompt, userMessage string, temperature *float64, maxTokens *int) (string, error) {
	messages := []model.ChatCompletionMsg{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userMessage},
	}
	reqBody := model.ChatCompletionRequest{
		Model:       c.ModelName,
		Messages:    messages,
		Stream:      false,
		Temperature: temperature,
		MaxTokens:   maxTokens,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("序列化请求失败: %w", err)
	}

	url := c.BaseURL + "/chat/completions"
	req, err := http.NewRequest("POST", url, bytes.NewReader(jsonData))
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)

	start := time.Now()
	slog.Info("llm_request", "model", c.ModelName, "url", url, "body", ellipsis(string(jsonData), 2000))
	resp, err := llmHTTPClient.Do(req)
	if err != nil {
		slog.Error("llm_request_error", "model", c.ModelName, "error", err, "duration_ms", time.Since(start).Milliseconds())
		return "", fmt.Errorf("请求 LLM 失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("读取响应失败: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		bodyStr := string(body)
		if len(bodyStr) > 500 {
			bodyStr = bodyStr[:500] + "..."
		}
		slog.Error("llm_response_error", "status", resp.StatusCode, "body", bodyStr, "duration_ms", time.Since(start).Milliseconds())
		return "", fmt.Errorf("LLM API 返回 %d: %s", resp.StatusCode, bodyStr)
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("解析响应失败: %w, body: %s", err, string(body)[:min(len(body), 500)])
	}

	if len(result.Choices) > 0 {
		output := result.Choices[0].Message.Content
		slog.Info("llm_response", "model", c.ModelName, "duration_ms", time.Since(start).Milliseconds(), "output", ellipsis(output, 2000))
		return output, nil
	}

	return "", fmt.Errorf("LLM 返回空响应")
}

// ellipsis 截断文本，超过 maxLen 加 "..."
func ellipsis(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}