// Step02 · 提示词工程 —— 给模型一份"角色说明书"。
//
// 与 Step01 唯一的区别：调用模型前，先塞入一条 system 消息。
// 系统提示词采用经典三层结构：角色（你是谁）→ 背景（约束与风格）→ 任务（怎么干）。
// 同一个模型，换一份提示词就换了一种"人格"与输出格式 —— 这就是提示词工程的杠杆。
package step02

import (
	"fmt"

	"github.com/fuzhengwei/ToyAgent-Go/agent"
	"github.com/fuzhengwei/ToyAgent-Go/llm"
)

// Impl 提示词工程实现。
type Impl struct {
	model     llm.ChatModel
	lastTrace []map[string]any
}

// systemPrompt 三层结构系统提示词：角色 → 背景 → 任务与约束。
const systemPrompt = `# 角色
你是小傅哥的技术助理，专注讲清楚 AI Agent 的原理与工程实践。

# 背景
读者是正在学习智能体开发的 Java 工程师，熟悉后端技术，但对 AI 领域是新手。

# 任务与约束
1. 回答使用简体中文，口语化但专业。
2. 先给结论，再展开解释；涉及概念时给出生活化类比。
3. 若用户要求"用一句话"，必须严格只回答一句话。
4. 不确定的内容要如实说明，禁止编造。
`

// New 构造 Step02 智能体。
func New(model llm.ChatModel) agent.Agent {
	return &Impl{model: model}
}

// SystemPrompt 暴露系统提示词，供展示。
func SystemPrompt() string { return systemPrompt }

func (a *Impl) Name() string {
	return "Step02 · 提示词工程（系统提示词三层结构）"
}

func (a *Impl) Chat(input string) string {
	a.lastTrace = nil
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "prompt", "label": "注入系统提示词",
		"detail": fmt.Sprintf("角色/背景/任务 三层结构，长度 %d 字符", len([]rune(systemPrompt))),
	})
	answer := a.model.Chat([]llm.Message{
		llm.System(systemPrompt),
		llm.User(input),
	})
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "prompt", "label": "模型按人设作答",
		"detail": "输出风格由 system 消息约束",
	})
	return answer
}

func (a *Impl) Trace() []map[string]any {
	return agent.CopyTrace(a.lastTrace)
}

func (a *Impl) Reset() {}
