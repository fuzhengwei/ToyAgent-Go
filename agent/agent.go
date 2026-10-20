// Package agent 定义 ToyAgent 的智能体契约。
//
// 智能体契约 —— 整个 ToyAgent 的灵魂只有一个方法：
//
//	Chat(input string) string
//
// 输入一句话，输出一句话，中间发生了什么（思考、调工具、查记忆），
// 都是这个方法内部的策略 —— 26 个场景的演进，全是围绕这个方法的展开。
//
// Trace() 把中间过程暴露给调用方渲染（思考步骤、工具调用等），
// Reset() 供有状态场景（如记忆）清空上下文。
package agent

// Agent 智能体契约接口（对应 Java 版 cn.xiaofuge.ai.agent.Agent）。
type Agent interface {
	// Name 场景名称，用于展示，返回值与 Java 版 name() 一致。
	Name() string

	// Chat 智能体最小契约：输入用户消息，返回回答。
	Chat(input string) string

	// Trace 最近一次 Chat 的执行轨迹（供渲染时间线），无轨迹则返回空列表。
	// 每个元素形如 {"type": "...", "label": "...", "detail": "..."}，
	// type 取值与 Java 各场景一致（thought/action/observation/final 等）。
	Trace() []map[string]any

	// Reset 清空场景内部状态（如记忆），无状态场景为空实现。
	Reset()
}

// CopyTrace 返回轨迹的浅拷贝（对应 Java 的 List.copyOf）：
// 调用方拿到快照后，后续 Chat 重建轨迹不影响已返回的切片。
func CopyTrace(tr []map[string]any) []map[string]any {
	out := make([]map[string]any, len(tr))
	copy(out, tr)
	return out
}
