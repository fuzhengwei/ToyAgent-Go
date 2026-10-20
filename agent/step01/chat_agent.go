// Step01 · 对话智能体 —— 智能体的最小 MVP。
//
// 一个接口，一个方法，没有策略、没有工具、没有记忆。
// 它证明了一件事：所谓智能体，起点只是「把用户的话交给模型，把模型的话还给用户」。
// 后续 25 个场景，都是往这个最小骨架里加入一种能力。
package step01

import (
	"github.com/fuzhengwei/ToyAgent-Go/agent"
	"github.com/fuzhengwei/ToyAgent-Go/llm"
)

// Impl 最小 MVP 实现：每轮对话独立，输入 → 模型 → 输出。
type Impl struct {
	model     llm.ChatModel
	lastTrace []map[string]any
}

// New 构造 Step01 智能体。
func New(model llm.ChatModel) agent.Agent {
	return &Impl{model: model}
}

func (a *Impl) Name() string {
	return "Step01 · 对话智能体（最小 MVP）"
}

func (a *Impl) Chat(input string) string {
	// 全部实现就这一行核心逻辑
	return a.model.Chat([]llm.Message{llm.User(input)})
}

func (a *Impl) Trace() []map[string]any {
	return agent.CopyTrace(a.lastTrace)
}

func (a *Impl) Reset() {}
