package llm

// Message 一条对话消息。Role 取值：system / user / assistant。
//
// 用最纯的数据结构表达 —— 智能体与模型之间流动的永远只是
// 「角色 + 文本」，这就是一切复杂行为的原材料。
type Message struct {
	Role    string
	Content string
}

// System 构造 system 消息（角色设定与约束）。
func System(content string) Message { return Message{Role: "system", Content: content} }

// User 构造 user 消息（用户输入）。
func User(content string) Message { return Message{Role: "user", Content: content} }

// Assistant 构造 assistant 消息（模型此前的回复，回填进历史）。
func Assistant(content string) Message { return Message{Role: "assistant", Content: content} }
