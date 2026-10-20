// Step15 · Loop 运行时智能体 —— 生产级 Agent Loop 的骨架。
//
// Step03 的 ReAct 循环是"裸奔"的：没有保险丝、没有安全检查。
// 真实的智能体运行时（Runtime）在循环外面包了一层工程化外壳：
//   - 输入守卫 —— 拦截敏感/恶意请求（Prompt 注入防护）；
//   - 最大步数 —— 防止模型陷入死循环烧钱；
//   - 输出守卫 —— 对最终回答做合规过滤；
//   - 异常兜底 —— 工具失败不拖垮整个循环。
//
// 模型负责聪明，运行时负责可靠。
package step15

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/fuzhengwei/ToyAgent-Go/agent"
	"github.com/fuzhengwei/ToyAgent-Go/llm"
	"github.com/fuzhengwei/ToyAgent-Go/agent/step03"
)

const (
	maxSteps      = 4
	maxInputChars = 500
)

// Impl Loop 运行时实现。
type Impl struct {
	model     llm.ChatModel
	lastTrace []map[string]any
}

// blocked 敏感词黑名单（生产中应为规则引擎 + 分类模型）。
var blocked = []string{"密码", "攻击", "hack", "漏洞", "暴力", "炸药"}

const systemPrompt = `你是运行在受控运行时里的智能体。每轮只输出 JSON：
{"thought": "思考", "action": "get_weather|calculator", "action_input": "参数"}
或 {"thought": "思考", "final": "最终答案"}
可用工具：get_weather(city)、calculator(expression)
`

// New 构造 Step15 智能体。
func New(model llm.ChatModel) agent.Agent {
	return &Impl{model: model}
}

func (a *Impl) Name() string {
	return "Step15 · Agent Loop 运行时（守卫 + 保险丝）"
}

func (a *Impl) Chat(input string) string {
	a.lastTrace = nil

	// ---------- 输入守卫 ----------
	if len([]rune(input)) > maxInputChars {
		a.lastTrace = append(a.lastTrace, map[string]any{
			"type": "guard", "label": "⛔ 输入守卫拦截",
			"detail": fmt.Sprintf("输入超过 %d 字符上限", maxInputChars)})
		return "输入过长，已拦截（防止上下文爆炸与注入攻击面扩大）。"
	}
	lowerInput := strings.ToLower(input)
	for _, word := range blocked {
		if strings.Contains(lowerInput, word) {
			a.lastTrace = append(a.lastTrace, map[string]any{
				"type": "guard", "label": "⛔ 输入守卫拦截",
				"detail": fmt.Sprintf("命中敏感词: %s（Prompt 注入/安全防护）", word)})
			return "该请求涉及受限内容，已被输入守卫拦截。"
		}
	}
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "guard", "label": "🛡️ 输入守卫通过",
		"detail": fmt.Sprintf("长度 %d 字符，未命中敏感词", len([]rune(input)))})

	// ---------- Agent Loop ----------
	messages := []llm.Message{llm.System(systemPrompt), llm.User(input)}

	for step := 1; step <= maxSteps; step++ {
		raw, err := llm.SafeChat(a.model, messages)
		if err != nil {
			a.lastTrace = append(a.lastTrace, map[string]any{
				"type": "warn", "label": "⚠️ 异常兜底",
				"detail": "模型调用失败: " + err.Error()})
			return "运行时异常已兜底，请稍后重试。"
		}
		parsed := llm.TryParse(strings.TrimSpace(raw))
		if parsed != nil {
			if v, ok := parsed["thought"]; ok && v != nil {
				a.lastTrace = append(a.lastTrace, map[string]any{
					"type": "thought", "label": fmt.Sprintf("Thought %d", step),
					"detail": fmt.Sprintf("%v", v)})
			}
		}

		fin := llm.Str(parsed, "final")
		if parsed != nil && fin != "" {
			// ---------- 输出守卫 ----------
			lowerFin := strings.ToLower(fin)
			for _, word := range blocked {
				if strings.Contains(lowerFin, word) {
					a.lastTrace = append(a.lastTrace, map[string]any{
						"type": "guard", "label": "⛔ 输出守卫拦截",
						"detail": "回答命中敏感词，已替换为安全话术"})
					return "回答未通过输出合规检查，已按安全策略处理。"
				}
			}
			a.lastTrace = append(a.lastTrace, map[string]any{
				"type": "final", "label": "✅ 输出守卫通过 · Final",
				"detail": fmt.Sprintf("循环共执行 %d 步", step)})
			return fin
		}

		action := llm.Str(parsed, "action")
		actionInput := llm.Str(parsed, "action_input")
		a.lastTrace = append(a.lastTrace, map[string]any{
			"type": "action", "label": fmt.Sprintf("Action %d", step),
			"detail": action + "(" + actionInput + ")"})

		// 工具异常不拖垮循环
		observation, execErr := execute(action, actionInput)
		if execErr != nil {
			observation = "工具执行异常: " + execErr.Error()
			a.lastTrace = append(a.lastTrace, map[string]any{
				"type": "warn", "label": "⚠️ 工具异常", "detail": observation})
		}
		a.lastTrace = append(a.lastTrace, map[string]any{
			"type": "observation", "label": fmt.Sprintf("Observation %d", step), "detail": observation})

		messages = append(messages, llm.Assistant(raw))
		messages = append(messages, llm.User("Observation: "+observation))
	}

	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "guard", "label": "🔥 保险丝熔断",
		"detail": fmt.Sprintf("达到最大步数 %d，强制终止循环", maxSteps)})
	return fmt.Sprintf("已达到最大执行步数（%d），为控制成本与风险强制终止。", maxSteps)
}

// execute 工具执行；err 非空对应 Java 抛异常（调用方兜底）。
func execute(action, input string) (string, error) {
	switch action {
	case "get_weather":
		return step03.MockWeather(input), nil
	case "calculator":
		m := calcRe.FindStringSubmatch(orEmpty(input))
		if m == nil {
			return "无法解析表达式", nil
		}
		aNum, _ := strconv.ParseFloat(m[1], 64)
		bNum, _ := strconv.ParseFloat(m[3], 64)
		switch m[2] {
		case "+":
			return llm.FormatDouble(aNum + bNum), nil
		case "-":
			return llm.FormatDouble(aNum - bNum), nil
		case "*":
			return llm.FormatDouble(aNum * bNum), nil
		case "/":
			if bNum == 0 {
				return "除数不能为 0", nil
			}
			return llm.FormatDouble(aNum / bNum), nil
		default:
			return "不支持的运算符", nil
		}
	default:
		if action == "" {
			return "未知工具: ", nil
		}
		return "未知工具: " + action, nil
	}
}

// calcRe 对齐 Java matches() 全串匹配语义。
var calcRe = regexp.MustCompile(`^\s*(\d+(?:\.\d+)?)\s*([+\-*/])\s*(\d+(?:\.\d+)?)\s*$`)

func orEmpty(s string) string {
	if s == "" {
		return "null" // 对齐 Java String.valueOf(null)
	}
	return s
}

func (a *Impl) Trace() []map[string]any {
	return agent.CopyTrace(a.lastTrace)
}

func (a *Impl) Reset() {}
