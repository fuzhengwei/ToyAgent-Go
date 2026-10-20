// Step05 · 记忆智能体 —— 让对话"有来有回"。
//
// Step01 每轮都是失忆的。本场景引入短期记忆：把历史消息一并放进
// 上下文窗口发给模型。当历史超过阈值时，用"摘要压缩"保护窗口 ——
// 滑动窗口 + 摘要，就是上下文工程（Context Engineering）的基础盘。
//
// 记忆的本质不是数据库，而是：每一次请求，都带上该带的历史。
package step05

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/fuzhengwei/ToyAgent-Go/agent"
	"github.com/fuzhengwei/ToyAgent-Go/llm"
)

// maxRounds 保留的最大历史轮数（一条 user + 一条 assistant 算一轮）。
const maxRounds = 4

// Impl 记忆智能体实现：历史与摘要是实例字段，同一实例多轮 Chat 复用状态。
type Impl struct {
	model     llm.ChatModel
	history   []llm.Message
	summary   string
	lastTrace []map[string]any
}

const systemPrompt = `你是一个有记忆的聊天智能体。请基于"对话历史 + 压缩摘要 + 新输入"回答，
用户提到的个人信息（如名字）要在后续对话中保持一致。
`

// New 构造 Step05 智能体。
func New(model llm.ChatModel) agent.Agent {
	return &Impl{model: model}
}

func (a *Impl) Name() string {
	return "Step05 · 记忆系统（多轮记忆 + 上下文压缩）"
}

func (a *Impl) Chat(input string) string {
	a.lastTrace = nil
	a.history = append(a.history, llm.User(input))

	// 历史超限 → 压缩：把最旧的两轮合并为一条摘要
	if len(a.history) > maxRounds*2 {
		a.compress()
	}

	// 组装上下文：系统提示词 + 摘要 + 历史
	context := []llm.Message{llm.System(systemPrompt)}
	if a.summary != "" {
		context = append(context, llm.System("【历史摘要】"+a.summary))
	}
	context = append(context, a.history...)

	detail := fmt.Sprintf("携带 %d 条历史消息", len(a.history))
	if a.summary != "" {
		detail += " + 1 条压缩摘要"
	}
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "memory", "label": "记忆窗口", "detail": detail})

	answer, err := llm.SafeChat(a.model, context)
	if err != nil {
		answer = err.Error()
	}
	a.history = append(a.history, llm.Assistant(answer))

	tail := ""
	if a.summary != "" {
		tail = " + 摘要"
	}
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "final", "label": "回答完成",
		"detail": "本条回答同时看到：系统提示词" + tail + " + 全部历史 + 新输入"})
	return answer
}

// compress 上下文压缩：把最旧两轮摘成一句话，历史重新滑窗。
func (a *Impl) compress() {
	old1 := a.history[0]
	old2 := a.history[1]
	a.history = a.history[2:]
	digest := clip(old1.Content) + "；随后助手回答了 " + clip(old2.Content)
	if a.summary == "" {
		a.summary = digest
	} else {
		a.summary = a.summary + " → " + digest
	}
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "compress", "label": "触发上下文压缩",
		"detail": fmt.Sprintf("最旧 2 轮已摘要化，摘要长度 %d 字符", len([]rune(a.summary)))})
}

func clip(s string) string {
	one := strings.TrimSpace(wsRe.ReplaceAllString(s, " "))
	r := []rune(one)
	if len(r) > 40 {
		return string(r[:40]) + "…"
	}
	return one
}

var wsRe = regexp.MustCompile(`\s+`)

func (a *Impl) Trace() []map[string]any {
	return agent.CopyTrace(a.lastTrace)
}

func (a *Impl) Reset() {
	a.history = nil
	a.summary = ""
}
