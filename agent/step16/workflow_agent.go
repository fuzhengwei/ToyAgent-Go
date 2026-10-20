// Step16 · 工作流智能体 —— LangGraph 式的状态机编排。
//
// ReAct 是"模型说了算"的自由循环，但很多业务需要"流程说了算"的
// 确定性编排。把智能体拆成节点（Node）与边（Edge）：
// 每个节点是一段能力（分类/工具/问答/润色），条件边决定流转，
// 状态（State）在节点间传递 —— 这就是 LangGraph 的核心思想，
// 也是生产系统中"可控性"与"灵活性"的平衡点。
package step16

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

// Impl 工作流状态机实现。
type Impl struct {
	model     llm.ChatModel
	lastTrace []map[string]any
}

// New 构造 Step16 智能体。
func New(model llm.ChatModel) agent.Agent {
	return &Impl{model: model}
}

func (a *Impl) Name() string {
	return "Step16 · 工作流状态机（LangGraph 思想）"
}

func (a *Impl) Chat(input string) string {
	a.lastTrace = nil

	// State：在节点之间流转的共享状态
	state := map[string]any{
		"input":       input,
		"answer":      "",
		"observation": "",
	}

	// 固定入口：classify 节点
	a.runNode("classify", state)

	// 条件边：按分类结果路由
	next, _ := state["next"].(string)
	if next == "" {
		next = "faq"
	}
	target := "faq 节点"
	if next == "tool" {
		target = "tool 节点"
	}
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "edge", "label": "条件边",
		"detail": "classify --" + next + "--> " + target})

	a.runNode(next, state)
	a.runNode("polish", state) // 汇合点：统一润色
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "edge", "label": "END",
		"detail": "执行路径: classify → " + next + " → polish → END"})

	return fmt.Sprintf("%v", state["answer"])
}

// runNode 节点执行器：每个节点在 State 上读输入、写产出。
func (a *Impl) runNode(node string, state map[string]any) {
	input := fmt.Sprintf("%v", state["input"])
	nodePrompt := "[节点:" + node + "] "
	switch node {
	case "classify":
		// 意图分类节点：模型输出路由决策
		raw, err := llm.SafeChat(a.model, []llm.Message{
			llm.System(nodePrompt + `你是意图分类器，只输出 JSON：{"next": "faq|tool", "reason": "一句话"}
涉及天气或四则运算 → tool，其他 → faq
`),
			llm.User(input)})
		if err != nil {
			raw = ""
		}
		parsed := llm.TryParse(strings.TrimSpace(raw))
		next := llm.Str(parsed, "next")
		if next == "" {
			next = "faq"
		}
		state["next"] = next
		a.lastTrace = append(a.lastTrace, map[string]any{
			"type": "node", "label": "🔹 节点 classify", "detail": "路由决策 → " + next})
	case "tool":
		m := calcRe.FindStringSubmatch(input)
		var observation string
		if m != nil {
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
				observation = "计算结果 失败"
			} else if r == math.Floor(r) {
				observation = "计算结果 " + strconv.FormatInt(int64(r), 10)
			} else {
				observation = "计算结果 " + fmt.Sprintf("%v", r)
			}
		} else {
			city := "北京"
			for _, c := range []string{"上海", "杭州", "成都"} {
				if strings.Contains(input, c) {
					city = c
					break
				}
			}
			observation = "「" + city + "」" + step03.MockWeather(city)
		}
		state["observation"] = observation
		a.lastTrace = append(a.lastTrace, map[string]any{
			"type": "node", "label": "🔹 节点 tool", "detail": observation})
	case "faq":
		answer, err := llm.SafeChat(a.model, []llm.Message{
			llm.System(nodePrompt + "你是 FAQ 问答节点，直接简洁回答常见问题。"),
			llm.User(input)})
		if err != nil {
			answer = err.Error()
		}
		state["answer"] = answer
		a.lastTrace = append(a.lastTrace, map[string]any{
			"type": "node", "label": "🔹 节点 faq", "detail": clip(answer, 40)})
	case "polish":
		// 汇合润色节点：tool 分支的 observation 在这里转为最终回答
		obs := fmt.Sprintf("%v", state["observation"])
		base := fmt.Sprintf("%v", state["answer"])
		if obs != "" {
			base = "根据查询结果：" + obs
		}
		answer, err := llm.SafeChat(a.model, []llm.Message{
			llm.System(nodePrompt + fmt.Sprintf(`你是润色节点。基于前序节点产出整理最终回答，保持事实不变、表达自然、50 字以内。
前序产出: %s
`, base)),
			llm.User(input)})
		if err != nil {
			answer = err.Error()
		}
		state["answer"] = answer
		a.lastTrace = append(a.lastTrace, map[string]any{
			"type": "node", "label": "🔹 节点 polish", "detail": "最终回答已生成"})
	default:
		a.lastTrace = append(a.lastTrace, map[string]any{
			"type": "warn", "label": "⚠️ 未知节点", "detail": node})
	}
}

func clip(s string, n int) string {
	one := strings.TrimSpace(wsRe.ReplaceAllString(s, " "))
	r := []rune(one)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return one
}

var (
	wsRe   = regexp.MustCompile(`\s+`)
	calcRe = regexp.MustCompile(`(\d+(?:\.\d+)?)\s*([+\-*/])\s*(\d+(?:\.\d+)?)`)
)

func (a *Impl) Trace() []map[string]any {
	return agent.CopyTrace(a.lastTrace)
}

func (a *Impl) Reset() {}
