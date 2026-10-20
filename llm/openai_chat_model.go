package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// OpenAiChatModel OpenAI 兼容协议的模型实现（DeepSeek / 通义 / 智谱 / 火山方舟等均适用）。
//
// 核心就一次 HTTP 调用：POST {base-url}/chat/completions，
// 请求体是 messages 数组，响应里取 choices[0].message.content。
// 所有"大模型"对你而言都只是这一个端点。
type OpenAiChatModel struct {
	baseUrl string
	apiKey  string
	model   string
	client  *http.Client
}

// NewOpenAiChatModel 构造：baseUrl 去掉末尾斜杠。
func NewOpenAiChatModel(baseUrl, apiKey, model string) *OpenAiChatModel {
	return &OpenAiChatModel{
		baseUrl: strings.TrimRight(baseUrl, "/"),
		apiKey:  apiKey,
		model:   model,
		// Java 版为 connect 15s + 请求 120s 两级超时；标准库合并为一个总超时
		client: &http.Client{Timeout: 120 * time.Second},
	}
}

// Chat 执行一次对话补全。失败时 panic（由 llm.SafeChat 或场景兜底捕获）。
func (m *OpenAiChatModel) Chat(messages []Message) string {
	// 1. 组装请求体：{"model": "...", "messages": [{"role": "...", "content": "..."}]}
	msgs := make([]map[string]string, 0, len(messages))
	for _, msg := range messages {
		msgs = append(msgs, map[string]string{"role": msg.Role, "content": msg.Content})
	}
	body := map[string]any{
		"model":       m.model,
		"messages":    msgs,
		"temperature": 0.7,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		panic(fmt.Errorf("模型调用异常: %v", err))
	}

	req, err := http.NewRequest(http.MethodPost, m.baseUrl+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		panic(fmt.Errorf("模型调用异常: %v", err))
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+m.apiKey)

	// 2. 调用模型
	resp, err := m.client.Do(req)
	if err != nil {
		panic(fmt.Errorf("模型调用异常: %v", err))
	}
	defer resp.Body.Close()
	buf := new(bytes.Buffer)
	_, _ = buf.ReadFrom(resp.Body)
	respBody := buf.String()

	if resp.StatusCode != 200 {
		panic(fmt.Errorf("模型调用失败 HTTP %d: %s", resp.StatusCode, snippet(respBody)))
	}

	// 3. 解析响应：choices[0].message.content
	var parsed struct {
		Choices []struct {
			Message struct {
				Content any `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal([]byte(respBody), &parsed); err != nil {
		panic(fmt.Errorf("模型调用异常: %v", err))
	}
	if len(parsed.Choices) == 0 {
		panic(fmt.Errorf("模型调用异常: 响应中无 choices"))
	}
	content := parsed.Choices[0].Message.Content
	if content == nil {
		return "null" // 对齐 Java String.valueOf(null)
	}
	return fmt.Sprintf("%v", content)
}

// snippet 非 200 时附响应前 300 字符（按字符截断，对齐 Java substring 语义）。
func snippet(s string) string {
	if s == "" {
		return ""
	}
	r := []rune(s)
	if len(r) > 300 {
		return string(r[:300]) + "..."
	}
	return s
}
