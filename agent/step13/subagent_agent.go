// Step13 · 子代理 —— 主智能体的「可派遣分身」。
//
// 主代理不必亲自做所有事：把子任务委托给带独立上下文的子代理，结果回填主会话。
// 两种派遣方式：
//   - spawn：全新实例，只有职责提示词，看不到父会话 —— 适合无状态专项任务；
//   - fork：复制父会话上下文再出发 —— 适合「接着当前话题继续做」。
//
// 与 Step12 多智能体的区别：Step12 是固定流水线（规划→研究→写作→审查），
// 角色间结构化协作；本步是主代理动态派遣 —— 模型自己决定派谁、派几个、
// 带不带上下文，即 Claude Code 的 Task 工具。
//
// 试试本场景的对话（体会 spawn 与 fork 的上下文差异）：
//
//	记住：我最喜欢紫色
//	fork 一个子代理，让它写一句贺词   —— 子代理继承父会话，贺词里带紫色
//	再 spawn 一个子代理写贺词        —— 全新实例，贺词里没有紫色
//	派个研究员调研一下 Agent 趋势    —— spawn 专项任务
package step13

import (
	"fmt"
	"strings"

	"github.com/fuzhengwei/ToyAgent-Go/agent"
	"github.com/fuzhengwei/ToyAgent-Go/llm"
)

// childSpec 子代理档案：职责提示词（真实场景里每个子代理还可挂自己的工具集）。
type childSpec struct{ specialty string }

// children 子代理注册表（保持插入顺序，等价 Java LinkedHashMap）。
var children = []struct {
	id   string
	spec childSpec
}{
	{"researcher", childSpec{"你是调研子代理。根据任务输出 3 条要点式调研结论，不写代码。"}},
	{"writer", childSpec{"你是写作子代理。根据任务输出一段 50 字以内的短文，不解释。"}},
}

// systemPromptPrefix 主代理系统提示词。
const systemPromptPrefix = `你是主智能体，可以把子任务委托给子代理。可用子代理：
- researcher：调研汇总
- writer：文案写作
派遣方式：spawn = 全新实例（无父会话上下文）；fork = 继承父会话上下文。
需要委托时只输出 JSON：{"tool": "dispatch_subagent", "name": "researcher", "mode": "spawn", "task": "子任务描述"}
不需要委托时，直接用自然语言回答，不要输出 JSON。
`

// Impl 子代理场景实现：parentTranscript 是父会话上下文，fork 时被子代理继承。
type Impl struct {
	model            llm.ChatModel
	parentTranscript []string
	lastTrace        []map[string]any
}

// New 构造 Step13 智能体。
func New(model llm.ChatModel) agent.Agent {
	return &Impl{model: model}
}

func (a *Impl) Name() string {
	return "Step13 · 子代理（spawn 独立上下文 / fork 继承上下文）"
}

func (a *Impl) Chat(input string) string {
	a.lastTrace = nil
	a.parentTranscript = append(a.parentTranscript, "用户: "+input)

	var ids []string
	for _, c := range children {
		ids = append(ids, c.id)
	}
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "tool", "label": "主代理就绪",
		"detail": fmt.Sprintf("可派遣子代理：[%s]（父会话上下文 %d 条）", strings.Join(ids, ", "), len(a.parentTranscript))})

	ctx := ""
	if len(a.parentTranscript) > 0 {
		ctx = "\n【父会话上下文】\n" + strings.Join(a.parentTranscript, "\n")
	}
	messages := []llm.Message{
		llm.System(systemPromptPrefix + ctx),
		llm.User(input),
	}

	for round := 1; round <= 3; round++ {
		raw, err := llm.SafeChat(a.model, messages)
		if err != nil {
			raw = err.Error()
		}
		decision := llm.TryParse(strings.TrimSpace(raw))

		if decision != nil && llm.Str(decision, "tool") != "" {
			name := llm.Str(decision, "name")
			mode := llm.Str(decision, "mode")
			task := llm.Str(decision, "task")
			a.lastTrace = append(a.lastTrace, map[string]any{
				"type": "action", "label": "主代理派遣子代理",
				"detail": fmt.Sprintf("subagent=%s  mode=%s  task=%s", name, mode, task)})

			result := a.dispatch(name, mode, task)
			a.lastTrace = append(a.lastTrace, map[string]any{
				"type": "observation", "label": "子代理返回", "detail": result})

			messages = append(messages, llm.Assistant(raw))
			messages = append(messages, llm.User(fmt.Sprintf("子代理 %s(%s) 执行结果: %s", name, mode, result)))
			continue
		}

		a.lastTrace = append(a.lastTrace, map[string]any{
			"type": "final", "label": "最终回答", "detail": "主代理汇总子代理结果，向用户作答"})
		a.parentTranscript = append(a.parentTranscript, "助手: "+raw)
		return raw
	}
	return "已达最大轮数，循环终止。"
}

// dispatch 派遣：spawn 给全新上下文；fork 复制父会话再出发。
func (a *Impl) dispatch(name, mode, task string) string {
	var spec *childSpec
	for _, c := range children {
		if c.id == name {
			spec = &c.spec
			break
		}
	}
	if spec == nil {
		var ids []string
		for _, c := range children {
			ids = append(ids, c.id)
		}
		return "没有叫 " + name + " 的子代理。可用：[" + strings.Join(ids, ", ") + "]"
	}
	fork := strings.EqualFold(mode, "fork")

	var sys strings.Builder
	sys.WriteString("【子代理身份】" + spec.specialty)
	if fork {
		sys.WriteString("\n【继承的父会话上下文】\n" + strings.Join(a.parentTranscript, "\n"))
	} else {
		sys.WriteString("\n（spawn 模式：全新实例，看不到父会话）")
	}

	childMessages := []llm.Message{
		llm.System(sys.String()),
		llm.User(task),
	}
	report, err := llm.SafeChat(a.model, childMessages)
	if err != nil {
		report = err.Error()
	}
	report = strings.TrimSpace(report)

	ctxInfo := "全新（0 条历史）"
	if fork {
		ctxInfo = fmt.Sprintf("继承父会话 %d 条", len(a.parentTranscript))
	}
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "tool", "label": "子代理运行（" + mode + "）",
		"detail": fmt.Sprintf("%s 收到任务「%s」，上下文 %s", name, task, ctxInfo)})
	return report
}

func (a *Impl) Trace() []map[string]any {
	return agent.CopyTrace(a.lastTrace)
}

func (a *Impl) Reset() {
	a.parentTranscript = nil
}
