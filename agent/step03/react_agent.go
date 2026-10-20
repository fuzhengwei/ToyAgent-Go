// Step03 · ReAct 智能体 —— 让模型学会"边想边做"。
//
// ReAct = Reasoning + Acting。模型不再一次作答，而是输出结构化的
// Thought（思考）→ Action（行动）→ 等待 Observation（观察结果），
// 智能体执行工具后把结果喂回模型，循环直到模型给出 Final Answer。
// 这是几乎所有智能体框架（LangChain、Agent Loop）的内核。
package step03

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/fuzhengwei/ToyAgent-Go/agent"
	"github.com/fuzhengwei/ToyAgent-Go/llm"
)

// maxSteps 循环保险丝：最多推理步数。
const maxSteps = 5

// Impl ReAct 智能体实现。
type Impl struct {
	model     llm.ChatModel
	lastTrace []map[string]any
}

// systemPrompt 用系统提示词约定 ReAct 的输出协议（JSON 形式，便于解析）。
const systemPrompt = `你是一个使用 ReAct 模式解决问题的智能体。
每一轮只输出一个 JSON 对象，格式二选一：
1. 继续行动：{"thought": "你的思考", "action": "工具名", "action_input": "参数"}
2. 给出答案：{"thought": "你的思考", "final": "最终答案"}
可用工具：
- get_weather(city): 查询城市天气
- calculator(expression): 计算四则运算，如 12*8
- get_time(): 获取当前时间
不要输出 JSON 以外的内容。
`

// New 构造 Step03 智能体。
func New(model llm.ChatModel) agent.Agent {
	return &Impl{model: model}
}

func (a *Impl) Name() string {
	return "Step03 · ReAct 智能体（思考-行动-观察循环）"
}

func (a *Impl) Chat(input string) string {
	a.lastTrace = nil
	messages := []llm.Message{llm.System(systemPrompt), llm.User(input)}

	for step := 1; step <= maxSteps; step++ {
		// 1. 模型思考并决策
		raw, err := llm.SafeChat(a.model, messages)
		if err != nil {
			// 模型没按协议输出 → 视为最终答案直接返回
			a.lastTrace = append(a.lastTrace, map[string]any{
				"type": "final", "label": "Final Answer", "detail": err.Error()})
			return err.Error()
		}
		raw = strings.TrimSpace(raw)
		raw = stripCodeFence(raw)
		parsed := llm.TryParse(raw)
		if parsed == nil {
			// 模型没按协议输出 → 视为最终答案直接返回
			a.lastTrace = append(a.lastTrace, map[string]any{
				"type": "final", "label": "Final Answer", "detail": raw})
			return raw
		}

		thought := llm.Str(parsed, "thought")
		a.lastTrace = append(a.lastTrace, map[string]any{
			"type": "thought", "label": fmt.Sprintf("Thought %d", step), "detail": thought})

		// 2. 有 final → 循环结束
		if fin := llm.Str(parsed, "final"); fin != "" {
			a.lastTrace = append(a.lastTrace, map[string]any{
				"type": "final", "label": "Final Answer", "detail": fin})
			return fin
		}

		// 3. 有 action → 执行工具，把观察结果喂回模型
		action := llm.Str(parsed, "action")
		actionInput := llm.Str(parsed, "action_input")
		a.lastTrace = append(a.lastTrace, map[string]any{
			"type": "action", "label": fmt.Sprintf("Action %d", step),
			"detail": action + "(" + actionInput + ")"})

		observation := a.executeTool(action, actionInput)
		a.lastTrace = append(a.lastTrace, map[string]any{
			"type": "observation", "label": fmt.Sprintf("Observation %d", step), "detail": observation})

		messages = append(messages, llm.Assistant(raw))
		messages = append(messages, llm.User("Observation: "+observation))
	}
	msg := fmt.Sprintf("已达最大步数 %d，循环终止（生产系统必须有这个保险丝）。", maxSteps)
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "guard", "label": "达到最大步数", "detail": msg})
	return msg
}

// executeTool 工具注册表：本场景仅 3 个本地工具，执行器就是一个 switch。
func (a *Impl) executeTool(action, input string) (result string) {
	defer func() {
		if r := recover(); r != nil {
			result = fmt.Sprintf("工具执行失败: %v", r)
		}
	}()
	switch action {
	case "get_weather":
		return MockWeather(input)
	case "calculator":
		v, err := calc(input)
		if err != nil {
			return "工具执行失败: " + err.Error()
		}
		return llm.FormatDouble(v)
	case "get_time":
		return time.Now().Format("2006-01-02 15:04:05")
	default:
		if action == "" {
			return "未知工具: "
		}
		return "未知工具: " + action
	}
}

// calc 教学用四则运算求值（仅支持 a op b）；解析失败返回 error（对应 Java 抛异常）。
func calc(expr string) (float64, error) {
	m := calcRe.FindStringSubmatch(expr)
	if m == nil {
		return 0, fmt.Errorf("无法解析表达式: %s", expr)
	}
	a, err1 := strconv.ParseFloat(m[1], 64)
	b, err2 := strconv.ParseFloat(m[3], 64)
	if err1 != nil || err2 != nil {
		return 0, fmt.Errorf("无法解析表达式: %s", expr)
	}
	switch m[2] {
	case "+":
		return a + b, nil
	case "-":
		return a - b, nil
	case "*":
		return a * b, nil
	case "/":
		if b == 0 {
			return 0, fmt.Errorf("除数不能为 0")
		}
		return a / b, nil
	default:
		return 0, fmt.Errorf("不支持的运算符 %s", m[2])
	}
}

// calcRe 对齐 Java 的 matches() 全串匹配语义（RE2 需显式锚定）。
var calcRe = regexp.MustCompile(`^\s*(\d+(?:\.\d+)?)\s*([+\-*/])\s*(\d+(?:\.\d+)?)\s*$`)

func stripCodeFence(s string) string {
	return llm.StripFence(s)
}

func (a *Impl) Trace() []map[string]any {
	return agent.CopyTrace(a.lastTrace)
}

func (a *Impl) Reset() {}

// weatherData 本地 Mock 天气数据。
var weatherData = map[string]string{
	"北京": "晴，26℃", "上海": "多云，28℃", "广州": "雷阵雨，31℃",
	"深圳": "阵雨，30℃", "杭州": "晴，27℃", "成都": "阴，22℃",
}

// MockWeather 本地 Mock 天气查询（被 step04/06/09/10/15/18 跨场景复用，对应 Java 的 MockWeather.query）。
func MockWeather(city string) string {
	key := strings.TrimSpace(city)
	if v, ok := weatherData[key]; ok {
		return v
	}
	return "未收录城市 " + key + " 的数据"
}
