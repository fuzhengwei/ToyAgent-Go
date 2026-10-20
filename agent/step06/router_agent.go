// Step06 · 意图路由智能体 —— Agent 的大脑：先识别意图，再决策走向。
//
// 复杂系统不会把所有问题都丢给一个循环：先让模型做一次"意图识别"
// （输出 JSON 分类），再由决策中枢把请求路由到不同处理器 ——
// 天气走工具、算式走计算器、闲聊走对话。这正是 Agent Loop 中
// Router/Dispatcher 的原型，也是多智能体系统编排的前置能力。
package step06

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/fuzhengwei/ToyAgent-Go/agent"
	"github.com/fuzhengwei/ToyAgent-Go/llm"
	"github.com/fuzhengwei/ToyAgent-Go/agent/step03"
)

// Impl 意图路由实现。
type Impl struct {
	model     llm.ChatModel
	lastTrace []map[string]any
}

const routerPrompt = `你是意图识别路由器。分析用户输入，只输出 JSON：
{"intent": "weather|math|chat", "reason": "一句话理由"}
- weather: 查询天气
- math: 四则运算
- chat: 其他一切对话
`

// New 构造 Step06 智能体。
func New(model llm.ChatModel) agent.Agent {
	return &Impl{model: model}
}

func (a *Impl) Name() string {
	return "Step06 · 意图识别与决策中枢（路由）"
}

func (a *Impl) Chat(input string) string {
	a.lastTrace = nil

	// 1. 意图识别
	raw, err := llm.SafeChat(a.model, []llm.Message{llm.System(routerPrompt), llm.User(input)})
	if err != nil {
		raw = ""
	}
	decision := llm.TryParse(strings.TrimSpace(raw))
	intent := llm.Str(decision, "intent")
	if intent == "" {
		intent = "chat"
	}
	reason := llm.Str(decision, "reason")
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "intent", "label": "意图识别",
		"detail": intent + "（" + reason + "）"})

	// 2. 按意图路由到不同处理器
	switch intent {
	case "weather":
		city := extractCity(input)
		a.lastTrace = append(a.lastTrace, map[string]any{
			"type": "action", "label": "路由 → 天气工具", "detail": "get_weather(" + city + ")"})
		result := step03.MockWeather(city)
		a.lastTrace = append(a.lastTrace, map[string]any{
			"type": "observation", "label": "工具返回", "detail": result})
		return "「" + city + "」现在" + result + "。"
	case "math":
		a.lastTrace = append(a.lastTrace, map[string]any{
			"type": "action", "label": "路由 → 计算器", "detail": input})
		return "计算结果：" + calc(input) + "。"
	default:
		a.lastTrace = append(a.lastTrace, map[string]any{
			"type": "action", "label": "路由 → 对话模型", "detail": "无工具直答"})
		answer, err := llm.SafeChat(a.model, []llm.Message{
			llm.System("你是一个友好的中文聊天助手。"),
			llm.User(input)})
		if err != nil {
			return err.Error()
		}
		return answer
	}
}

func extractCity(text string) string {
	for _, city := range []string{"北京", "上海", "广州", "深圳", "杭州", "成都"} {
		if strings.Contains(text, city) {
			return city
		}
	}
	return "北京"
}

func calc(expr string) string {
	m := calcRe.FindStringSubmatch(expr)
	if m == nil {
		return "未识别的算式"
	}
	aNum, _ := strconv.ParseFloat(m[1], 64)
	bNum, _ := strconv.ParseFloat(m[3], 64)
	var r float64
	switch m[2] {
	case "+":
		r = aNum + bNum
	case "-":
		r = aNum - bNum
	case "*":
		r = aNum * bNum
	case "/":
		if bNum == 0 {
			r = math.NaN()
		} else {
			r = aNum / bNum
		}
	default:
		r = math.NaN()
	}
	if math.IsNaN(r) {
		return "无法计算"
	}
	if r == math.Floor(r) {
		return strconv.FormatInt(int64(r), 10)
	}
	return fmt.Sprintf("%v", r)
}

// calcRe 对齐 Java 的 find() 包含语义。
var calcRe = regexp.MustCompile(`(\d+(?:\.\d+)?)\s*([+\-*/])\s*(\d+(?:\.\d+)?)`)

func (a *Impl) Trace() []map[string]any {
	return agent.CopyTrace(a.lastTrace)
}

func (a *Impl) Reset() {}
