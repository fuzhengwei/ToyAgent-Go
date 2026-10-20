// Step12 · 多智能体协作 —— 从一个大脑到一支团队。
//
// 单个智能体上下文有限、职责混杂。多智能体模式按角色拆分：
// 规划者（Planner）拆解任务 → 专家（Worker）各司其职 →
// 审查员（Reviewer）质量把关 → 主持者（Orchestrator）汇总交付。
//
// 每个角色都是一个独立的模型实例（不同的系统提示词），
// 协作的本质是：结构化的消息在角色之间流动。
package step12

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/fuzhengwei/ToyAgent-Go/agent"
	"github.com/fuzhengwei/ToyAgent-Go/llm"
)

// Impl 多智能体协作实现。
type Impl struct {
	model     llm.ChatModel
	lastTrace []map[string]any
}

// New 构造 Step12 智能体。
func New(model llm.ChatModel) agent.Agent {
	return &Impl{model: model}
}

func (a *Impl) Name() string {
	return "Step12 · 多智能体协作（规划-执行-审查）"
}

func (a *Impl) Chat(input string) string {
	a.lastTrace = nil

	// 1. 规划者：拆解任务
	planRaw, err := llm.SafeChat(a.model, []llm.Message{
		llm.System(`你是规划者（Planner）。把用户任务拆解为三个角色的执行计划，只输出 JSON：
{"plan": [{"role": "研究员", "task": "..."}, {"role": "写手", "task": "..."}, {"role": "审查员", "task": "..."}]}
`),
		llm.User(input)})
	if err != nil {
		planRaw = ""
	}
	planObj := llm.TryParse(strings.TrimSpace(planRaw))
	var plan []any
	if planObj != nil {
		if p, ok := planObj["plan"].([]any); ok {
			plan = p
		}
	}
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "agent", "label": "🧭 规划者",
		"detail": fmt.Sprintf("产出 %d 步计划：%s", len(plan), describe(plan))})

	// 2. 研究员：收集素材
	notes, err := llm.SafeChat(a.model, []llm.Message{
		llm.System("你是研究员（Researcher），围绕任务输出 3 条以内研究要点，每条一句话。"),
		llm.User(input)})
	if err != nil {
		notes = err.Error()
	}
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "agent", "label": "🔬 研究员", "detail": clip(notes, 60)})

	// 3. 写手：基于素材成稿
	draft, err := llm.SafeChat(a.model, []llm.Message{
		llm.System(fmt.Sprintf(`你是写手（Writer）。基于研究笔记写一段 100 字以内的短文，主题如下。
研究笔记: %s
`, notes)),
		llm.User(input)})
	if err != nil {
		draft = err.Error()
	}
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "agent", "label": "✍️ 写手", "detail": clip(draft, 60)})

	// 4. 审查员：质量把关
	reviewRaw, err := llm.SafeChat(a.model, []llm.Message{
		llm.System(`你是审查员（Reviewer）。审查短文，只输出 JSON：
{"pass": true/false, "comment": "一句话点评"}
`),
		llm.User(draft)})
	if err != nil {
		reviewRaw = ""
	}
	review := llm.TryParse(strings.TrimSpace(reviewRaw))
	pass := false
	if review != nil {
		if b, ok := review["pass"].(bool); ok {
			pass = b
		}
	}
	comment := llm.Str(review, "comment")
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type":   map[bool]string{true: "guard", false: "warn"}[pass],
		"label":  map[bool]string{true: "✅ 审查员：通过", false: "⚠️ 审查员：打回"}[pass],
		"detail": comment})

	// 5. 主持者汇总交付
	var finalAnswer string
	if pass {
		finalAnswer = draft
	} else {
		modified, err := llm.SafeChat(a.model, []llm.Message{
			llm.System("根据审查意见修改短文，直接输出修改后的版本。"),
			llm.User("原文: " + draft + "\n审查意见: " + comment)})
		if err != nil {
			finalAnswer = err.Error()
		} else {
			finalAnswer = modified
		}
	}
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "final", "label": "🏁 主持者汇总", "detail": "协作链路：规划 → 研究 → 写作 → 审查 → 交付"})
	return finalAnswer
}

func describe(plan []any) string {
	var sb strings.Builder
	for _, o := range plan {
		if m, ok := o.(map[string]any); ok {
			sb.WriteString(fmt.Sprintf("%v", m["role"]))
			sb.WriteString("→")
		}
	}
	s := sb.String()
	return strings.TrimSuffix(s, "→")
}

func clip(s string, n int) string {
	one := strings.TrimSpace(wsRe.ReplaceAllString(s, " "))
	r := []rune(one)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return one
}

var wsRe = regexp.MustCompile(`\s+`)

func (a *Impl) Trace() []map[string]any {
	return agent.CopyTrace(a.lastTrace)
}

func (a *Impl) Reset() {}
