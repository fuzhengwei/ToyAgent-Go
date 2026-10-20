// Step20 · 审批门禁 —— 高危操作先过人，批准才放行。
//
// 四个教学点：
//  1. 风险分级：每个工具声明风险等级，安全工具（只读）自动放行，
//     高危工具（写文件/发邮件/执行命令）必须过门禁；
//  2. 三档模式：AUTO_MANUAL_SAFE（本场景默认）/ ALWAYS_ASK / FULL_AUTO，决定门禁松紧；
//  3. 会话级免审记忆：「批准并记住」后，同工具本会话内自动放行 ——
//     信任一次，不再重复打扰；
//  4. 超时默认 DENY：生产里审批有超时，超时即拒绝（宁可不执行，不可乱执行）。
package step20

import (
	"fmt"
	"strings"

	"github.com/fuzhengwei/ToyAgent-Go/agent"
	"github.com/fuzhengwei/ToyAgent-Go/llm"
)

// mode 审批模式三档。
type mode int

const (
	autoManualSafe mode = iota // 安全自动放行，高危问人（默认）
	alwaysAsk                  // 一切外部动作都要审批
	fullAuto                   // 全自动（仅教学演示，生产慎用）
)

func (m mode) String() string {
	switch m {
	case alwaysAsk:
		return "ALWAYS_ASK"
	case fullAuto:
		return "FULL_AUTO"
	default:
		return "AUTO_MANUAL_SAFE"
	}
}

const systemPrompt = `你是一个接入了多种工具的智能体。可用工具：
- get_weather(city)：查天气【安全·只读】
- send_email(to, content)：发邮件【高危·写操作】
- delete_file(path)：删文件【高危·写操作】
需要工具时只输出 JSON：{"tool": "工具名", "arguments": "参数摘要"}
无需工具时只输出 JSON：{"final": "最终答案"}。不要输出 JSON 以外内容。`

// Impl 审批门禁实现：pendingTool/pendingArgs 非空表示审批挂起。
type Impl struct {
	model        llm.ChatModel
	history      []llm.Message
	trustedTools map[string]bool // 会话级免审名单：批准并记住的高危工具
	pendingTool  string
	pendingArgs  string
	approveMode  mode
	lastTrace    []map[string]any
}

// New 构造 Step20 智能体。
func New(model llm.ChatModel) agent.Agent {
	return &Impl{model: model, trustedTools: map[string]bool{}, approveMode: autoManualSafe}
}

func (a *Impl) Name() string {
	return "Step20 · 审批门禁（高危操作先过人：批准 / 拒绝 / 批准并记住）"
}

func (a *Impl) Chat(input string) string {
	a.lastTrace = nil

	// ===== 分支一：有挂起审批 —— 本条输入是人的决策，不进模型 =====
	if a.pendingTool != "" {
		tool := a.pendingTool
		args := a.pendingArgs
		a.pendingTool = ""
		return a.resolveApproval(tool, args, input)
	}

	// ===== 分支二：正常对话 =====
	a.history = append(a.history, llm.User(input))
	return a.decideAndAct(input)
}

func (a *Impl) decideAndAct(input string) string {
	req := []llm.Message{llm.System(systemPrompt)}
	req = append(req, a.history...)

	raw, err := llm.SafeChat(a.model, req)
	if err != nil {
		raw = ""
	}
	parsed := llm.TryParse(strings.TrimSpace(raw))
	if parsed == nil {
		a.history = append(a.history, llm.Assistant(raw))
		a.lastTrace = append(a.lastTrace, map[string]any{
			"type": "final", "label": "直接作答", "detail": "无工具需求"})
		return raw
	}

	tool := llm.Str(parsed, "tool")
	if tool == "" {
		answer := orRaw(llm.Str(parsed, "final"), raw)
		a.history = append(a.history, llm.Assistant(answer))
		a.lastTrace = append(a.lastTrace, map[string]any{
			"type": "final", "label": "直接作答", "detail": "无需工具"})
		return answer
	}

	args := ""
	if v, ok := parsed["arguments"]; ok && v != nil {
		args = javaStr(v)
	}
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "action", "label": "模型发起工具调用", "detail": tool + " " + args})

	// ===== 权限评估：风险分级 + 模式 + 会话免审记忆 =====
	dangerous := isDangerous(tool)
	needAsk := a.approveMode == alwaysAsk ||
		(a.approveMode == autoManualSafe && dangerous && !a.trustedTools[tool])
	trustedNote := ""
	if a.trustedTools[tool] {
		trustedNote = " · 命中会话免审"
	}
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "thought", "label": "权限矩阵评估",
		"detail": fmt.Sprintf("%s → %s · 模式 %s%s", tool,
			map[bool]string{true: "高危", false: "安全"}[dangerous], a.approveMode, trustedNote)})

	if needAsk {
		a.pendingTool = tool
		a.pendingArgs = args
		a.lastTrace = append(a.lastTrace, map[string]any{
			"type": "guard", "label": "审批挂起 PENDING_APPROVAL",
			"detail": "高危操作等待人工决策。生产版有审批超时 —— 超时默认 DENY，宁可不执行"})
		return "⚠️ 即将执行高危操作：" + tool + "\n参数：" + args + "\n\n" +
			"请回复：「批准」执行 ｜「拒绝」取消 ｜「批准并记住」本会话内该工具免审"
	}

	result := execute(tool, args)
	label := "自动放行（安全工具）"
	if a.trustedTools[tool] && dangerous {
		label = "自动放行（会话免审）"
	}
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "observation", "label": label, "detail": result})
	return a.finish(tool, result)
}

// resolveApproval 处理人的审批决策。
func (a *Impl) resolveApproval(tool, args, decision string) string {
	d := strings.TrimSpace(decision)
	switch {
	case strings.HasPrefix(d, "批准并记住"):
		a.trustedTools[tool] = true
		a.lastTrace = append(a.lastTrace, map[string]any{
			"type": "guard", "label": "人工决策 · 批准并记住",
			"detail": tool + " 加入会话级免审名单，本会话后续调用自动放行"})
	case strings.HasPrefix(d, "批准"):
		a.lastTrace = append(a.lastTrace, map[string]any{
			"type": "guard", "label": "人工决策 · 批准",
			"detail": "仅本次放行，下次仍会询问"})
	case strings.HasPrefix(d, "拒绝"):
		a.lastTrace = append(a.lastTrace, map[string]any{
			"type": "guard", "label": "人工决策 · 拒绝",
			"detail": "高危操作被拦截，任务终止（生产版超时未审批同样按 DENY 处理）"})
		a.history = append(a.history, llm.User("审批结果："+tool+" 被拒绝"))
		return "已拒绝执行 " + tool + "。操作未发生 —— 审批门禁的意义就在于：人的否决权永远在模型之上。"
	default:
		a.pendingTool = tool
		a.pendingArgs = args
		a.lastTrace = append(a.lastTrace, map[string]any{
			"type": "guard", "label": "无法识别的决策",
			"detail": "审批仍挂起，请回复：批准 / 拒绝 / 批准并记住"})
		return "没看懂你的决定。请回复：「批准」｜「拒绝」｜「批准并记住」"
	}
	result := execute(tool, args)
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "observation", "label": "审批放行 · 执行", "detail": result})
	a.history = append(a.history, llm.User("审批结果："+tool+" 已批准并执行"))
	return a.finish(tool, result)
}

// finish 工具执行后，让模型基于结果收尾。
func (a *Impl) finish(tool, result string) string {
	req := []llm.Message{llm.System(systemPrompt +
		"\n（工具 " + tool + " 的执行结果：" + result + "。请据此作答。）")}
	req = append(req, a.history...)
	raw, err := llm.SafeChat(a.model, req)
	if err != nil {
		raw = ""
	}
	parsed := llm.TryParse(strings.TrimSpace(raw))
	answer := raw
	if parsed != nil {
		answer = orRaw(llm.Str(parsed, "final"), raw)
	}
	a.history = append(a.history, llm.Assistant(answer))
	return answer
}

// -------------------------------------------------------------- 风险与执行

// isDangerous 风险分级：写出去的操作都是高危，只读是安全。
func isDangerous(tool string) bool {
	switch tool {
	case "send_email", "delete_file", "shell_execute", "publish_article":
		return true
	default:
		return false
	}
}

// execute 模拟执行（教学：不真发邮件、不真删文件）。
func execute(tool, args string) string {
	switch tool {
	case "send_email":
		return "已发送（模拟）：" + args
	case "delete_file":
		return "已删除（模拟）：" + args
	case "get_weather":
		return "晴，26℃（模拟数据）"
	default:
		return "已执行（模拟）：" + args
	}
}

// javaStr 对齐 Java String.valueOf(Object)：Map 输出 {k=v} 形式（顺序按 Go map 固定化处理）。
func javaStr(v any) string {
	if m, ok := v.(map[string]any); ok {
		var parts []string
		for k, val := range m {
			parts = append(parts, k+"="+fmt.Sprintf("%v", val))
		}
		return "{" + strings.Join(parts, ", ") + "}"
	}
	return fmt.Sprintf("%v", v)
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
	a.trustedTools = map[string]bool{}
	a.pendingTool = ""
	a.pendingArgs = ""
}
