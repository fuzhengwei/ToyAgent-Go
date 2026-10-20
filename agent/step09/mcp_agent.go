// Step09 · MCP 智能体 —— 工具的标准化接口。
//
// Step04 的工具是"写死在智能体里"的。MCP（Model Context Protocol）
// 把工具侧拆成一个独立服务：工具在 Server 中统一注册，Client 通过
// 两个标准方法使用它们 —— tools/list（发现）与 tools/call（调用）。
// 从此工具与智能体解耦：换智能体不用重写工具，接新工具不用改智能体。
//
// 本场景用两个结构体/接口模拟这一协议（真实 MCP 是 JSON-RPC over stdio/HTTP）。
package step09

import (
	"fmt"
	"strings"
	"time"

	"github.com/fuzhengwei/ToyAgent-Go/agent"
	"github.com/fuzhengwei/ToyAgent-Go/llm"
	"github.com/fuzhengwei/ToyAgent-Go/agent/step03"
)

// Impl MCP 智能体实现。
type Impl struct {
	model     llm.ChatModel
	client    *mcpClient
	lastTrace []map[string]any
}

// New 构造 Step09 智能体：一个 MCP Server 挂载两个工具，Client 负责协议交互。
func New(model llm.ChatModel) agent.Agent {
	server := &weatherTimeServer{}
	return &Impl{model: model, client: &mcpClient{server: server}}
}

func (a *Impl) Name() string {
	return "Step09 · MCP 协议（工具标准化接口）"
}

func (a *Impl) Chat(input string) string {
	a.lastTrace = nil

	// 1. 工具发现：从 MCP Server 拉取工具清单，拼进提示词
	tools := a.client.listTools()
	var names []string
	for _, t := range tools {
		names = append(names, t["name"])
	}
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "mcp", "label": "tools/list 工具发现",
		"detail": fmt.Sprintf("从 MCP Server 动态获取 %d 个工具: %s", len(tools), strings.Join(names, ", "))})

	var toolDocs strings.Builder
	for _, t := range tools {
		toolDocs.WriteString("- " + t["name"] + "(" + t["parameters"] + "): " + t["description"] + "\n")
	}

	decidePrompt := fmt.Sprintf(`你是可以调用 MCP 工具的智能体。可用工具：
%s
需要工具时只输出 JSON：{"method": "tools/call", "params": {"name": "工具名", "arguments": {"参数": "值"}}}
否则直接自然语言回答。
`, toolDocs.String())

	// 2. 模型决策
	raw, err := llm.SafeChat(a.model, []llm.Message{llm.System(decidePrompt), llm.User(input)})
	if err != nil {
		a.lastTrace = append(a.lastTrace, map[string]any{
			"type": "final", "label": "直接回答", "detail": "本次无需工具"})
		return err.Error()
	}
	decision := llm.TryParse(strings.TrimSpace(raw))

	if llm.Str(decision, "method") == "tools/call" {
		params, _ := decision["params"].(map[string]any)
		if params == nil {
			params = map[string]any{}
		}
		name := fmt.Sprintf("%v", params["name"])
		args := llm.Write(params["arguments"])
		a.lastTrace = append(a.lastTrace, map[string]any{
			"type": "tool", "label": "tools/call 工具调用", "detail": name + " " + args})

		// 3. 经由 MCP Client 标准化调用
		result := a.client.callTool(name, args)
		a.lastTrace = append(a.lastTrace, map[string]any{
			"type": "observation", "label": "Server 返回", "detail": result})

		answer, err := llm.SafeChat(a.model, []llm.Message{
			llm.System("基于工具返回的事实回答，简洁准确。"),
			llm.User(input + "\n工具结果: " + result)})
		if err != nil {
			answer = err.Error()
		}
		a.lastTrace = append(a.lastTrace, map[string]any{
			"type": "final", "label": "生成回答", "detail": "工具与智能体已解耦：Server 换实现，智能体零改动"})
		return answer
	}

	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "final", "label": "直接回答", "detail": "本次无需工具"})
	return raw
}

func (a *Impl) Trace() []map[string]any {
	return agent.CopyTrace(a.lastTrace)
}

func (a *Impl) Reset() {}

// ------------------------------------------------------------------ MCP 协议三件套

// toolDescriptor 工具描述符：MCP 世界里工具的标准"名片"。
type toolDescriptor struct {
	name, description, parameters string
}

// mcpServer MCP Server 接口：工具的标准化宿主（真实实现为独立进程，JSON-RPC 通信）。
type mcpServer interface {
	tools() []toolDescriptor
	call(toolName, argsJson string) string
}

// mcpClient MCP Client：负责 tools/list 与 tools/call 两个标准方法。
type mcpClient struct {
	server mcpServer
}

func (c *mcpClient) listTools() []map[string]string {
	var out []map[string]string
	for _, t := range c.server.tools() {
		out = append(out, map[string]string{
			"name": t.name, "description": t.description, "parameters": t.parameters,
		})
	}
	return out
}

func (c *mcpClient) callTool(name, argsJson string) string {
	for _, t := range c.server.tools() {
		if t.name == name {
			return c.server.call(name, argsJson)
		}
	}
	return "MCP 错误: 工具不存在 " + name
}

// weatherTimeServer 示例 Server：挂载天气与时间两个工具。
type weatherTimeServer struct{}

func (s *weatherTimeServer) tools() []toolDescriptor {
	return []toolDescriptor{
		{"get_weather", "查询城市实时天气", "city"},
		{"get_time", "获取当前时间", ""},
	}
}

func (s *weatherTimeServer) call(toolName, argsJson string) (result string) {
	defer func() {
		if r := recover(); r != nil {
			result = fmt.Sprintf("调用失败: %v", r)
		}
	}()
	args := llm.TryParse(orEmptyObj(argsJson))
	if args == nil {
		args = map[string]any{}
	}
	switch toolName {
	case "get_weather":
		return step03.MockWeather(llm.Str(args, "city"))
	case "get_time":
		return time.Now().Format("2006-01-02 15:04:05")
	default:
		return "未知工具"
	}
}

func orEmptyObj(s string) string {
	if s == "" {
		return "{}"
	}
	return s
}
