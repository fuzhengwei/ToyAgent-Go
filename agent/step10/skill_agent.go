// Step10 · Skills 技能智能体 —— 工具的组合与复用。
//
// 单个工具解决单步问题，真实任务往往是多步组合。
// Skill 是"可沉淀、可复用的能力包"，按抽象层级分三层：
//   - L0 提示词技能 —— 纯 Prompt 模板，零工具；
//   - L1 工具组合 —— 固定调用一组工具并拼装结果；
//   - L2 子流程 —— 一个完整的 ReAct 循环被封成一项技能。
//
// 模型按语义选择技能，智能体负责执行 —— 这就是 Claude Skills /
// 插件体系的编排思想。
package step10

import (
	"strings"
	"time"

	"github.com/fuzhengwei/ToyAgent-Go/agent"
	"github.com/fuzhengwei/ToyAgent-Go/llm"
	"github.com/fuzhengwei/ToyAgent-Go/agent/step03"
)

// Impl 技能编排实现。
type Impl struct {
	model     llm.ChatModel
	lastTrace []map[string]any
}

const selectPrompt = `你是技能选择器。当前已注册技能：
- travel_plan(L1): 旅行攻略 —— 组合天气查询与时间工具生成出行建议
- weather_report(L1): 天气播报 —— 调用天气工具
- free_chat(L0): 自由聊天 —— 纯提示词技能，直接回答

根据用户输入只输出 JSON：{"skill": "技能名", "reason": "一句话理由"}
`

// New 构造 Step10 智能体。
func New(model llm.ChatModel) agent.Agent {
	return &Impl{model: model}
}

func (a *Impl) Name() string {
	return "Step10 · 技能编排（Skills L0/L1/L2）"
}

func (a *Impl) Chat(input string) string {
	a.lastTrace = nil

	// 1. 技能选择
	raw, err := llm.SafeChat(a.model, []llm.Message{llm.System(selectPrompt), llm.User(input)})
	if err != nil {
		raw = ""
	}
	decision := llm.TryParse(strings.TrimSpace(raw))
	skill := llm.Str(decision, "skill")
	if skill == "" {
		skill = "free_chat"
	}
	reason := llm.Str(decision, "reason")
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "skill", "label": "技能选择", "detail": skill + "（" + reason + "）"})

	// 2. 技能执行
	switch skill {
	case "travel_plan":
		return a.travelPlan(input) // L1: 多工具组合
	case "weather_report":
		return a.weatherReport(input) // L1: 单工具
	default:
		return a.freeChat(input) // L0: 纯提示词
	}
}

// travelPlan L1 技能：组合天气 + 时间两个工具，产出结构化攻略。
func (a *Impl) travelPlan(input string) string {
	city := "北京"
	for _, c := range []string{"杭州", "上海", "成都", "广州", "深圳"} {
		if strings.Contains(input, c) {
			city = c
			break
		}
	}
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "action", "label": "L1 组合工具 1/2", "detail": "get_weather(" + city + ")"})
	weather := step03.MockWeather(city)
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "observation", "label": "工具返回", "detail": weather})
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "action", "label": "L1 组合工具 2/2", "detail": "get_time()"})
	timeNow := time.Now().Format("2006-01-02 15:04")
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "observation", "label": "工具返回", "detail": timeNow})

	answer, err := llm.SafeChat(a.model, []llm.Message{
		llm.System("你是旅行顾问。基于给定的天气与时间信息，输出简短出行攻略（3 条以内要点）。\n"),
		llm.User(input + "\n天气: " + weather + "\n当前时间: " + timeNow)})
	if err != nil {
		answer = err.Error()
	}
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "final", "label": "L1 技能完成", "detail": "两件工具的结果已拼装进提示词"})
	return answer
}

// weatherReport L1 技能：单工具播报。
func (a *Impl) weatherReport(input string) string {
	city := "北京"
	if strings.Contains(input, "上海") {
		city = "上海"
	} else if strings.Contains(input, "杭州") {
		city = "杭州"
	}
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "action", "label": "L1 调用工具", "detail": "get_weather(" + city + ")"})
	weather := step03.MockWeather(city)
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "final", "label": "播报完成", "detail": weather})
	return "「" + city + "」" + weather + "。"
}

// freeChat L0 技能：纯提示词，不碰工具。
func (a *Impl) freeChat(input string) string {
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "final", "label": "L0 提示词技能", "detail": "无工具，直接对话"})
	answer, err := llm.SafeChat(a.model, []llm.Message{
		llm.System("你是友好的中文助手，回答保持简洁。"),
		llm.User(input)})
	if err != nil {
		return err.Error()
	}
	return answer
}

func (a *Impl) Trace() []map[string]any {
	return agent.CopyTrace(a.lastTrace)
}

func (a *Impl) Reset() {}
