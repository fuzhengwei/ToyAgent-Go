// Step14 · A2A 协作 —— 代理之间的「标准化网络协议」。
//
// Step12/13 的多代理都活在同一个进程里，主代理直接构造子代理。
// A2A（Agent-to-Agent）解决的是跨进程、跨主人的协作：
//   - 名片发现（Agent Card）：每个代理对外公布一份 JSON 名片 —— 我是谁、
//     住在哪个 URL、会什么技能；
//   - 标准化请求：协作不靠猜接口，靠统一信封 —— 任务描述 + task_id + 状态机
//     （submitted → working → completed/failed）；
//   - 异步回执：发起方拿到 task_id 即可离开，结果异步取回。
//
// 与 Step13 子代理的区别：子代理是自家分身（进程内、共享宿主模型）；
// A2A 是外部同行（网络调用、对方有自己的模型与工具），
// 类比：请同事帮忙 vs 外包给合作公司。
//
// 试试本场景的对话：
//
//	发现一下附近的代理                   —— 拉取两张 Agent Card
//	让翻译代理把「你好，智能体」翻译成英文 —— a2a_send 委派，返回 task_id + 回执
//	问问天气代理明天适合户外运动吗        —— 按 skill 路由到天气代理
package step14

import (
	"crypto/rand"
	"fmt"
	"strings"

	"github.com/fuzhengwei/ToyAgent-Go/agent"
	"github.com/fuzhengwei/ToyAgent-Go/llm"
)

// agentCard 名片：A2A 协议里的发现单元 ——
// 生产中托管在 https://host/.well-known/agent.json，这里教学化简为 struct。
type agentCard struct {
	name, url, version string
	skills             []string
	description        string
}

// directory 本地目录：模拟网络上已注册的两个外部代理（真实实现是 HTTP 拉取名片）。
var directory = []agentCard{
	{"translator", "http://a2a.local/translator", "1.0.0", []string{"translate"}, "中英互译代理"},
	{"weather", "http://a2a.local/weather", "2.1.0", []string{"forecast"}, "天气预报代理"},
}

const systemPrompt = `你是 ToyAgent 主代理，可通过 A2A 协议与外部代理协作。协作前先发现名片。
查看目录时只输出 JSON：{"tool": "a2a_discover"}
委派任务时只输出 JSON：{"tool": "a2a_send", "agent": "translator|weather", "task": "任务描述"}
翻译类任务派给 translator，天气类任务派给 weather。拿到结果后用自然语言回答。`

// Impl A2A 场景实现。
type Impl struct {
	model     llm.ChatModel
	lastTrace []map[string]any
	// tasks 已发起的协作任务：id → 描述（生产中持久化并轮询状态）。
	tasks map[string]string
	seq   int
}

// New 构造 Step14 智能体。
func New(model llm.ChatModel) agent.Agent {
	return &Impl{model: model, tasks: map[string]string{}}
}

func (a *Impl) Name() string {
	return "Step14 · A2A 协作（Agent Card 名片发现 · task_id 信封 · 异步回执）"
}

func (a *Impl) Chat(input string) string {
	a.lastTrace = nil
	messages := []llm.Message{llm.System(systemPrompt), llm.User(input)}

	for round := 1; round <= 3; round++ {
		raw, err := llm.SafeChat(a.model, messages)
		if err != nil {
			raw = err.Error()
		}
		decision := llm.TryParse(strings.TrimSpace(raw))

		if decision != nil && llm.Str(decision, "tool") != "" {
			tool := llm.Str(decision, "tool")
			var result string
			if tool == "a2a_discover" {
				result = a.discover()
			} else {
				result = a.send(llm.Str(decision, "agent"), llm.Str(decision, "task"))
			}
			messages = append(messages, llm.Assistant(raw))
			messages = append(messages, llm.User("工具执行结果: "+result))
			continue
		}

		a.lastTrace = append(a.lastTrace, map[string]any{
			"type": "final", "label": "最终回答", "detail": "汇总协作结果，向用户作答"})
		return raw
	}
	return "已达最大轮数，循环终止。"
}

// discover a2a_discover：拉取目录里所有代理的名片（生产中 GET /.well-known/agent.json）。
func (a *Impl) discover() string {
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "action", "label": "a2a_discover · 名片发现",
		"detail": fmt.Sprintf("拉取 %d 份 Agent Card（模拟网络请求）", len(directory))})
	var sb strings.Builder
	sb.WriteString("[")
	for i, c := range directory {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(fmt.Sprintf("\n  {\"name\": \"%s\", \"url\": \"%s\", \"version\": \"%s\", \"skills\": [%s]}",
			c.name, c.url, c.version, strings.Join(c.skills, ", ")))
	}
	sb.WriteString("\n]")
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "observation", "label": "收到名片",
		"detail": "translator（中英互译） · weather（天气预报）"})
	return sb.String()
}

// send a2a_send：按名片路由委派请求，模拟 submitted → completed 状态机并回执。
func (a *Impl) send(agent, task string) string {
	var card *agentCard
	for i := range directory {
		if directory[i].name == agent {
			card = &directory[i]
			break
		}
	}
	if card == nil {
		a.lastTrace = append(a.lastTrace, map[string]any{
			"type": "guard", "label": "a2a_send · 路由失败",
			"detail": "目录中没有 " + agent + "，请先 a2a_discover"})
		return "{\"error\": \"unknown agent: " + agent + "\"}"
	}
	a.seq++
	taskId := fmt.Sprintf("task-%03d-%s", a.seq, randHex4())
	a.tasks[taskId] = agent + ": " + task

	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "action", "label": "a2a_send · 发起协作",
		"detail": fmt.Sprintf("→ %s @ %s  task=%s", card.name, card.url, task)})
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "thought", "label": "状态机 · submitted → working",
		"detail": "对方已受理 task_id=" + taskId + "（信封标准化：发起方只认 task_id，不关心对方内部实现）"})

	reply := handleRemotely(card, task)
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "observation", "label": "回执 · completed",
		"detail": "task_id=" + taskId + " 已完成：" + reply})
	return fmt.Sprintf("{\"task_id\": \"%s\", \"status\": \"completed\", \"result\": \"%s\"}", taskId, reply)
}

// handleRemotely 模拟远端代理按 skill 处理（真实实现：POST JSON-RPC / HTTP task 请求）。
func handleRemotely(card *agentCard, task string) string {
	switch card.name {
	case "translator":
		return "翻译结果（模拟远端模型）：Hello, Agent."
	case "weather":
		return "明日晴，22-28℃，东南风 3 级，适合户外运动（模拟远端数据）"
	default:
		return "（模拟远端响应）"
	}
}

// randHex4 生成 4 位十六进制随机串（对齐 Java UUID 前 4 位）。
func randHex4() string {
	b := make([]byte, 2)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}

func (a *Impl) Trace() []map[string]any {
	return agent.CopyTrace(a.lastTrace)
}

func (a *Impl) Reset() {
	a.tasks = map[string]string{}
	a.seq = 0
}
