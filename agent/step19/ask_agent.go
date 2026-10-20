// Step19 · 人工介入 —— ask_user_question：把「异步的人」翻译成「可等待的答复」。
//
// 教学版用「会话挂起 + 续答」实现：
//  1. 模型发现关键信息缺失时，调用 ask_user_question「工具」，turn 挂起，
//     问题持久化在会话状态里返回给用户；
//  2. 用户的下一条消息不再进模型，而是作为人工答复回填挂起的工具位，
//     turn 恢复继续执行。
//
// 生产版的差别只在「等待」的实现：用 Future 阻塞住执行线程等人，
// 教学版把等待摊开成两次请求 —— 人机协同的语义完全一致。
package step19

import (
	"strings"

	"github.com/fuzhengwei/ToyAgent-Go/agent"
	"github.com/fuzhengwei/ToyAgent-Go/llm"
)

// systemPrompt 系统提示词。
const systemPrompt = `你是一个懂得「何时该问人」的智能体。
当任务缺少关键信息（如预订缺少出发地、时间），不要猜测，主动向人提问：
只输出 JSON：{"ask": "你的问题（含可选选项）"}
信息完整时直接完成任务，只输出 JSON：{"final": "最终答案"}。不要输出 JSON 以外内容。`

// Impl 人工介入实现：pendingQuestion 非 nil 表示 turn 暂停，等待人工答复。
type Impl struct {
	model           llm.ChatModel
	history         []llm.Message
	pendingQuestion string
	lastTrace       []map[string]any
}

// New 构造 Step19 智能体。
func New(model llm.ChatModel) agent.Agent {
	return &Impl{model: model}
}

func (a *Impl) Name() string {
	return "Step19 · 人工介入（ask_user_question：提问挂起 → 答复续跑）"
}

func (a *Impl) Chat(input string) string {
	a.lastTrace = nil

	// ===== 分支一：有挂起问题 —— 本条输入是「人工答复」，不进模型 =====
	if a.pendingQuestion != "" {
		q := a.pendingQuestion
		a.pendingQuestion = ""
		a.lastTrace = append(a.lastTrace, map[string]any{
			"type": "observation", "label": "人工答复回填",
			"detail": "挂起问题「" + q + "」收到答复：" + input +
				"（生产版是 Future 阻塞-唤醒，教学版把等待摊开成两次请求）"})
		a.history = append(a.history, llm.User("人工答复："+input))

		answer := a.complete(q)
		a.history = append(a.history, llm.Assistant(answer))
		return answer
	}

	// ===== 分支二：正常对话 —— 模型可发起 ask_user_question =====
	a.history = append(a.history, llm.User(input))
	req := []llm.Message{llm.System(systemPrompt)}
	req = append(req, a.history...)

	raw, err := llm.SafeChat(a.model, req)
	if err != nil {
		raw = ""
	}
	parsed := llm.TryParse(strings.TrimSpace(raw))

	if parsed != nil {
		if question := llm.Str(parsed, "ask"); question != "" {
			// 模型主动向人提问：挂起 turn，问题存入会话状态
			a.pendingQuestion = question
			a.lastTrace = append(a.lastTrace, map[string]any{
				"type": "action", "label": "调用 ask_user_question",
				"detail": "turn 挂起，等待人工答复 —— 「异步的人」被翻译成了「可等待的工具结果」"})
			a.lastTrace = append(a.lastTrace, map[string]any{
				"type": "guard", "label": "会话挂起 PENDING_ASK",
				"detail": "问题已持久化在会话状态（重启不丢），下一条消息将作为答复回填"})
			return question + "\n\n（直接回复即可，我会带着你的答复继续任务。）"
		}
	}

	answer := raw
	if parsed != nil {
		answer = orRaw(llm.Str(parsed, "final"), raw)
	}
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "final", "label": "直接作答", "detail": "信息完整，无需人工介入"})
	a.history = append(a.history, llm.Assistant(answer))
	return answer
}

// complete 答复回填后，带着完整信息让模型继续完成任务（turn 恢复）。
func (a *Impl) complete(question string) string {
	req := []llm.Message{llm.System(systemPrompt +
		"\n（此前你曾向用户提问「" + question + "」，用户已答复，见最后一条人工答复。现在信息完整，直接完成任务并作答。）")}
	req = append(req, a.history...)
	raw, err := llm.SafeChat(a.model, req)
	if err != nil {
		raw = ""
	}
	parsed := llm.TryParse(strings.TrimSpace(raw))
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "final", "label": "turn 恢复 · 完成",
		"detail": "人工答复注入上下文，任务继续执行 —— 人机协同闭环"})
	if parsed != nil {
		return orRaw(llm.Str(parsed, "final"), raw)
	}
	return raw
}

func orRaw(v, raw string) string {
	if strings.TrimSpace(v) == "" {
		return raw
	}
	return v
}

func (a *Impl) Trace() []map[string]any {
	return agent.CopyTrace(a.lastTrace)
}

func (a *Impl) Reset() {
	a.history = nil
	a.pendingQuestion = ""
}
