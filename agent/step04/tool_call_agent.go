// Step04 · 工具调用（Function Calling）—— 给模型装上"手脚"。
//
// 与 Step03 手写 ReAct 循环不同，本场景展示标准的工具调用交互：
//  1. 在提示词中声明工具清单（名称、参数、用途）—— 即最简 Tool Schema；
//  2. 模型判断需要工具时，返回结构化调用请求 {"tool": "...", "arguments": {...}}；
//  3. 智能体执行工具，把结果作为新消息回填，再次请求模型生成最终回答。
//
// 生产中 OpenAI 的 tools/tool_calls 协议、MCP 协议，本质都是这三步。
package step04

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/fuzhengwei/ToyAgent-Go/agent"
	"github.com/fuzhengwei/ToyAgent-Go/llm"
	"github.com/fuzhengwei/ToyAgent-Go/agent/step03"
)

// Impl 工具调用实现。
type Impl struct {
	model     llm.ChatModel
	lastTrace []map[string]any
}

const systemPrompt = `你是一个可以调用工具的智能体。可用工具：
- get_weather(city): 查询指定城市的实时天气
- calculator(expression): 计算四则运算，如 12*8

决策规则：
- 需要工具时，只输出 JSON：{"tool": "工具名", "arguments": {"参数名": "值"}}
- 不需要工具时，直接用自然语言回答，不要输出 JSON。
`

// New 构造 Step04 智能体。
func New(model llm.ChatModel) agent.Agent {
	return &Impl{model: model}
}

func (a *Impl) Name() string {
	return "Step04 · 工具调用（Function Calling）"
}

func (a *Impl) Chat(input string) string {
	a.lastTrace = nil
	messages := []llm.Message{llm.System(systemPrompt), llm.User(input)}

	// 第一轮：模型决定"要不要用工具"
	first, err := llm.SafeChat(a.model, messages)
	if err != nil {
		a.lastTrace = append(a.lastTrace, map[string]any{
			"type": "final", "label": "无需工具，直接回答", "detail": "模型判断自身知识足够"})
		return err.Error()
	}
	first = strings.TrimSpace(first)
	decision := llm.TryParse(first)

	if tool := llm.Str(decision, "tool"); tool != "" {
		args := llm.Write(decision["arguments"])
		a.lastTrace = append(a.lastTrace, map[string]any{
			"type": "tool", "label": "模型发起工具调用", "detail": tool + " " + args})

		// 第二步：执行工具
		result := execute(tool, args)
		a.lastTrace = append(a.lastTrace, map[string]any{
			"type": "observation", "label": "工具返回结果", "detail": result})

		// 第三步：结果回填，让模型基于事实作答
		messages = append(messages, llm.Assistant(first))
		messages = append(messages, llm.User("工具 "+tool+" 的执行结果: "+result))
		answer, err := llm.SafeChat(a.model, messages)
		if err != nil {
			answer = err.Error()
		}
		a.lastTrace = append(a.lastTrace, map[string]any{
			"type": "final", "label": "基于工具结果生成回答", "detail": "模型拿到 Observation 后组织最终回答"})
		return answer
	}

	// 模型选择不调用工具，直接回答
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "final", "label": "无需工具，直接回答", "detail": "模型判断自身知识足够"})
	return first
}

func execute(tool, argsJson string) (result string) {
	defer func() {
		if r := recover(); r != nil {
			result = fmt.Sprintf("工具执行失败: %v", r)
		}
	}()
	args := llm.TryParse(orEmptyObj(argsJson))
	if args == nil {
		args = map[string]any{}
	}
	switch tool {
	case "get_weather":
		return step03.MockWeather(llm.Str(args, "city"))
	case "calculator":
		m := calcRe.FindStringSubmatch(orEmpty(llm.Str(args, "expression")))
		if m == nil {
			return "无法解析表达式"
		}
		aNum, _ := strconv.ParseFloat(m[1], 64)
		bNum, _ := strconv.ParseFloat(m[3], 64)
		switch m[2] {
		case "+":
			return llm.FormatDouble(aNum + bNum)
		case "-":
			return llm.FormatDouble(aNum - bNum)
		case "*":
			return llm.FormatDouble(aNum * bNum)
		case "/":
			if bNum == 0 {
				return "除数不能为 0"
			}
			return llm.FormatDouble(aNum / bNum)
		default:
			return "不支持的运算符"
		}
	default:
		return "未知工具: " + tool
	}
}

var calcRe = regexp.MustCompile(`^\s*(\d+(?:\.\d+)?)\s*([+\-*/])\s*(\d+(?:\.\d+)?)\s*$`)

// orEmptyObj 空 JSON 时回退 "{}"（对齐 Java Json.parse(argsJson == null ? "{}" : argsJson)）。
func orEmptyObj(s string) string {
	if s == "" {
		return "{}"
	}
	return s
}

func orEmpty(s string) string {
	if s == "" {
		return "null"
	}
	return s
}

func (a *Impl) Trace() []map[string]any {
	return agent.CopyTrace(a.lastTrace)
}

func (a *Impl) Reset() {}
