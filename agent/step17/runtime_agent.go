// Step17 · ReAct 运行时 —— turn/step 两级循环 + 上下文裁剪 + TurnEndReason。
//
// 三个核心机制：
//  1. turn/step 两级循环：一次 Chat() = 一个 turn；turn 内的循环是 step 循环。
//     与 Step03 的区别：工具执行完不结束回合，而是续步（mid-turn continuation）——
//     循环只能以 TurnEndReason 收场；
//  2. 上下文裁剪 truncateToBudget：每次调模型前估算 token（≈字符数/4），
//     超预算就从最老的非系统消息开始丢弃（保底留最近 4 条）。多轮对话后
//     在轨迹里能亲眼看到「上下文裁剪」事件；
//  3. 步数保险丝：单回合最多 maxStepsPerTurn 步，超限以 TurnEndReason.MaxSteps
//     收场（发「死循环测试」可亲眼看到保险丝起跳）。
//
// 工具直接复用 Step11 的 ToolRegistry —— 运行时与工具协议天然解耦。
package step17

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/fuzhengwei/ToyAgent-Go/agent"
	"github.com/fuzhengwei/ToyAgent-Go/llm"
	"github.com/fuzhengwei/ToyAgent-Go/agent/step11"
)

const (
	// maxStepsPerTurn 单回合步数保险丝（生产值通常为 50）。
	maxStepsPerTurn = 6
	// budgetTokens 上下文 token 预算（教学值调小以便演示，约 4 轮触发；估算规则：token ≈ 字符数 / 4）。
	budgetTokens = 100
	// keepMin 裁剪时保底保留的最近消息条数。
	keepMin = 4
)

// turnEndReasonKind 回合结束原因（对应 Java sealed interface TurnEndReason 的三种 record）。
//
// Go 无 sealed 类型，用 kind 标签 + 携带字段表达同一封闭代数类型；
// 新增结束原因时集中在此定义，下游 switch 显式覆盖，编译器无法穷尽检查，
// 这是与 Java 版的一处表达力差异。
type turnEndReason struct {
	kind    string // completed / maxSteps / error
	summary string // completed 摘要
	steps   int    // maxSteps 步数
	message string // error 信息
}

const (
	endCompleted = "completed"
	endMaxSteps  = "maxSteps"
	endError     = "error"
)

// systemPrompt 系统提示词（工具清单由注册表生成）。
const systemPrompt = `你是一个使用 ReAct 模式的智能体，运行在 turn/step 两级循环之上。
可用工具（由注册表生成）：
`

// Impl ReAct 运行时实现。
type Impl struct {
	model     llm.ChatModel
	registry  *step11.ToolRegistry
	history   []llm.Message // 跨对话的会话历史：运行时的上下文管理正是作用在它上面
	lastAnswer string
	lastTrace []map[string]any
}

// New 构造 Step17 智能体。
func New(model llm.ChatModel) agent.Agent {
	a := &Impl{model: model, registry: step11.NewToolRegistry()}
	a.registry.Register(&weatherTool{})
	a.registry.Register(&calculatorTool{})
	a.registry.Register(&echoTool{})
	return a
}

func (a *Impl) Name() string {
	return "Step17 · ReAct 运行时（turn/step + 上下文裁剪 + TurnEndReason）"
}

func (a *Impl) Chat(input string) string {
	a.lastTrace = nil
	a.history = append(a.history, llm.User(input))

	// ===== 机制 2：调模型前先做上下文预算裁剪 =====
	a.trimToBudget()

	// ===== 机制 1：turn 内的 step 循环，只能以 TurnEndReason 收场 =====
	reason := a.runTurn(input)

	// 回合结束原因入轨迹，并作为 user 消息回写历史（下一轮模型可见）
	var label string
	fused := reason.kind == endMaxSteps
	switch reason.kind {
	case endCompleted:
		label = "Turn 结束 · COMPLETED —— " + reason.summary
	case endMaxSteps:
		label = fmt.Sprintf("Turn 结束 · MAX_STEPS —— 达到保险丝 %d 步，强制收敛", reason.steps)
	default:
		label = "Turn 结束 · ERROR —— " + reason.message
	}
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type":   map[bool]string{true: "guard", false: "final"}[fused],
		"label":  label,
		"detail": "结束原因以封闭类型 TurnEndReason 统一表达，逐一处理"})
	return a.lastAnswer
}

// runTurn 一个 turn：循环执行 step，直到出现结束原因。
func (a *Impl) runTurn(input string) turnEndReason {
	for step := 1; step <= maxStepsPerTurn; step++ {
		// ---- 组装请求：系统提示词 + 会话历史（裁剪后） ----
		req := []llm.Message{llm.System(systemPrompt + a.registry.PromptCatalog() +
			"\n决策规则：\n- 需要工具时，只输出 JSON：" +
			"{\"thought\": \"思考\", \"action\": \"工具名\", \"action_input\": \"参数\"}\n" +
			"- 信息足够时，只输出 JSON：{\"thought\": \"思考\", \"final\": \"最终答案\"}\n" +
			"不要输出 JSON 以外的内容。")}
		req = append(req, a.history...)

		raw, err := llm.SafeChat(a.model, req)
		if err != nil {
			return turnEndReason{kind: endError, message: "模型调用失败: " + err.Error()}
		}
		parsed := llm.TryParse(strings.TrimSpace(raw))
		a.lastTrace = append(a.lastTrace, map[string]any{
			"type": "thought", "label": fmt.Sprintf("Step %d · 思考", step),
			"detail": firstLine(llm.Str(parsed, "thought"), raw)})

		if parsed == nil {
			// 模型失约：按协议约定视为最终答案
			a.lastAnswer = raw
			a.history = append(a.history, llm.Assistant(raw))
			return turnEndReason{kind: endCompleted, summary: "模型直接给出答案"}
		}

		if fin := llm.Str(parsed, "final"); fin != "" {
			a.lastAnswer = fin
			a.history = append(a.history, llm.Assistant(raw))
			return turnEndReason{kind: endCompleted, summary: "模型给出最终答案"}
		}

		// ---- 工具调用：执行后续步（不结束回合，这就是"续步"） ----
		action := llm.Str(parsed, "action")
		actionInput := llm.Str(parsed, "action_input")
		a.lastTrace = append(a.lastTrace, map[string]any{
			"type": "action", "label": fmt.Sprintf("Step %d · 行动", step),
			"detail": action + "(" + actionInput + ")"})

		var observation string
		tool := a.registry.Lookup(action)
		if tool == nil {
			observation = "未知工具: " + action + "。可用清单：\n" + a.registry.PromptCatalog()
		} else {
			// 与 Java 一致：三个参数键全部塞入，工具各取所需
			out, execErr := tool.Execute(map[string]any{
				"city": nz(actionInput), "expression": nz(actionInput), "text": nz(actionInput)})
			if execErr != nil {
				observation = "工具执行失败: " + execErr.Error()
			} else {
				observation = out
			}
		}
		a.lastTrace = append(a.lastTrace, map[string]any{
			"type": "observation", "label": fmt.Sprintf("Step %d · 观察", step), "detail": observation})

		// 观察结果进历史 —— 下一 step 模型自然"看见"，循环继续
		a.history = append(a.history, llm.Assistant(raw))
		a.history = append(a.history, llm.User("Observation: "+observation))
		a.trimToBudget()
	}
	a.lastAnswer = fmt.Sprintf("已达最大步数 %d，本回合被保险丝强制终止。"+
		"生产系统的 ReAct 循环必须有这层兜底（生产默认 50 步）。", maxStepsPerTurn)
	return turnEndReason{kind: endMaxSteps, steps: maxStepsPerTurn}
}

// ===== 机制 2：上下文预算裁剪（truncateToBudget） =====

// estimateTokens 估算 token：教学化近似 —— token ≈ 字符数 / 4。
func estimateTokens(msgs []llm.Message) int {
	n := 0
	for _, m := range msgs {
		n += utf8.RuneCountInString(m.Content) / 4
	}
	return n
}

func (a *Impl) trimToBudget() {
	if estimateTokens(a.history) <= budgetTokens || len(a.history) <= keepMin {
		return
	}
	// 从最老的消息开始丢（历史里没有系统消息，系统提示词在请求时单独加）
	drop := 0
	for estimateTokens(a.history) > budgetTokens && len(a.history) > keepMin {
		a.history = a.history[1:]
		drop++
	}
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "compress", "label": "上下文裁剪 truncateToBudget",
		"detail": fmt.Sprintf("预估 token 超过预算 %d，丢弃最早 %d 条消息，保留最近 %d 条，现估 %d tokens",
			budgetTokens, drop, len(a.history), estimateTokens(a.history))})
}

func (a *Impl) Trace() []map[string]any {
	return agent.CopyTrace(a.lastTrace)
}

func (a *Impl) Reset() {
	a.history = nil
	a.lastAnswer = ""
	a.registry.Clear()
	a.registry.Register(&weatherTool{})
	a.registry.Register(&calculatorTool{})
	a.registry.Register(&echoTool{})
}

// ------------------------------------------------------------------ 工具（复用 Step11 协议）

// weatherTool 天气工具。
type weatherTool struct{}

var weatherData = map[string]string{
	"北京": "晴，26℃", "上海": "多云，28℃", "广州": "雷阵雨，31℃",
	"深圳": "阵雨，30℃", "杭州": "晴，27℃", "成都": "阴，22℃",
}

func (t *weatherTool) Name() string        { return "get_weather" }
func (t *weatherTool) Description() string { return "查询指定城市的实时天气" }
func (t *weatherTool) Parameters() map[string]string {
	return map[string]string{"city": "string，城市名"}
}
func (t *weatherTool) Execute(args map[string]any) (string, error) {
	city := strings.TrimSpace(fmt.Sprintf("%v", argsOr(args, "city", "")))
	if v, ok := weatherData[city]; ok {
		return v, nil
	}
	return "未收录城市 " + city + " 的数据", nil
}
func (t *weatherTool) ConcurrencySafe() bool { return false }

// calculatorTool 四则运算工具。
type calculatorTool struct{}

func (t *calculatorTool) Name() string        { return "calculator" }
func (t *calculatorTool) Description() string { return "计算四则运算，如 12*8" }
func (t *calculatorTool) Parameters() map[string]string {
	return map[string]string{"expression": "string，算式"}
}
func (t *calculatorTool) Execute(args map[string]any) (string, error) {
	expr := fmt.Sprintf("%v", argsOr(args, "expression", ""))
	m := calcRe.FindStringSubmatch(expr)
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
}
func (t *calculatorTool) ConcurrencySafe() bool { return false }

// echoTool 永远回显的工具：专供「死循环测试」演示保险丝。
type echoTool struct{}

func (t *echoTool) Name() string        { return "echo" }
func (t *echoTool) Description() string { return "回显输入文本（演示用）" }
func (t *echoTool) Parameters() map[string]string {
	return map[string]string{"text": "string，要回显的内容"}
}
func (t *echoTool) Execute(args map[string]any) (string, error) {
	return "echo: " + fmt.Sprintf("%v", argsOr(args, "text", "")), nil
}
func (t *echoTool) ConcurrencySafe() bool { return false }

// ------------------------------------------------------------------ 工具方法

var calcRe = regexp.MustCompile(`^\s*(\d+(?:\.\d+)?)\s*([+\-*/])\s*(\d+(?:\.\d+)?)\s*$`)

func argsOr(args map[string]any, key string, def any) any {
	if v, ok := args[key]; ok && v != nil {
		return v
	}
	return def
}

func nz(s string) string {
	if s == "" {
		return ""
	}
	return s
}

// firstLine thought 为空时用原始输出第一行（对齐 Java firstLine）。
func firstLine(a, b string) string {
	s := b
	if strings.TrimSpace(a) != "" {
		s = a
	}
	if i := strings.IndexByte(s, '\n'); i > 0 {
		return s[:i]
	}
	return s
}
