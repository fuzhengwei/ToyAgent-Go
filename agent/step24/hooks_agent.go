// Step24 · Hooks 钩子体系 —— 挂在工具调用生命周期上的「插件化拦截点」。
//
// 工具执行前后并不是「裸奔」的：宿主在两个生命周期点暴露钩子位，任何符合签名的钩子都能挂上去：
//  1. before：工具执行前 —— 可拦截（DENY）或改写参数；
//  2. after：工具执行后 —— 可改写输出（脱敏、过滤、追加告警）。
//
// 三个要素：Hook（钩子本体）+ 匹配规则（挂在哪类工具上，即 HookMatcher）+
// 输出合并（多个 after 钩子按注册顺序依次加工，即 HookOutputMerger）。
//
// 关键设计思想：横切逻辑与工具解耦 —— 审计、脱敏、风控都不写进工具代码，
// 挂钩即生效，摘钩即消失；工具作者对此无感知。
//
// 试试本场景的对话：
//
//	查询：张三                          —— after 脱敏钩子把手机号/邮箱打码
//	发送：给 13812345678 发 密码是123456 —— before 风控钩子直接拦截
//	钩子：关  →  查询：张三              —— 摘掉钩子，明文裸奔，对比出钩子的价值
//	钩子：开                            —— 挂回钩子
//	审计日志                            —— before 审计钩子记下的每一次调用
package step24

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/fuzhengwei/ToyAgent-Go/agent"
	"github.com/fuzhengwei/ToyAgent-Go/llm"
)

const systemPrompt = `你是 ToyAgent 的钩子讲解员。用户可能询问钩子（Hooks）、拦截、脱敏、审计等话题。
只输出 JSON：{"final": "回答"}。请引导用户试用：「查询：张三」「发送：给 13812345678 发 密码是123456」「钩子：关」「审计日志」。`

// hook 钩子本体：name 标识、phase 挂载点（before/after）、match 匹配的工具名（* = 全部）。
// handler 返回 "" = 不干预；返回 "DENY:原因" = 拦截；否则用返回值替换原内容。
type hook struct {
	name    string
	phase   string
	match   []string
	handler func(tool, payload string) string
}

// Impl 钩子体系实现。
type Impl struct {
	model     llm.ChatModel
	lastTrace []map[string]any
	auditLog  []string // before 审计钩子的落点：生产中是审计库 / SIEM，教学中是内存切片
	hooks     []hook
	enabled   bool
}

// New 构造 Step24 智能体并安装出厂钩子。
func New(model llm.ChatModel) agent.Agent {
	a := &Impl{model: model, enabled: true}
	a.installDefaultHooks()
	return a
}

// installDefaultHooks 出厂自带三条钩子：审计（before/全部）、风控（before/发短信）、脱敏（after/全部）。
func (a *Impl) installDefaultHooks() {
	a.hooks = append(a.hooks,
		hook{"审计", "before", []string{"*"}, func(tool, payload string) string {
			a.auditLog = append(a.auditLog, time.Now().Format("15:04:05")+
				"  "+tool+"  args="+payload)
			return "" // 审计只记录，不干预
		}},
		hook{"风控-敏感外发", "before", []string{"send_sms"}, func(tool, payload string) string {
			if strings.Contains(payload, "密码") || strings.Contains(payload, "验证码") {
				return "DENY:短信内容含「密码/验证码」等敏感信息，禁止外发"
			}
			return ""
		}},
		hook{"脱敏", "after", []string{"*"}, func(tool, payload string) string {
			masked := mobileRe.ReplaceAllString(payload, "$1*********")
			masked = emailRe.ReplaceAllStringFunc(masked, func(m string) string {
				g := emailRe.FindStringSubmatch(m)
				return string([]rune(g[1])[0:1]) + "***@" + g[2]
			})
			if masked == payload {
				return ""
			}
			return masked
		}},
	)
}

var (
	mobileRe = regexp.MustCompile(`(1[3-9])\d{9}`)
	emailRe  = regexp.MustCompile(`([\w.]+)@([\w.]+)`)
)

func (a *Impl) Name() string {
	return "Step24 · Hooks 钩子体系（before 拦截/改写 · after 脱敏/审计，挂钩即生效）"
}

func (a *Impl) Chat(input string) string {
	a.lastTrace = nil

	// —— 钩子管理指令（宿主能力，不经过模型） ——
	if strings.HasPrefix(input, "钩子：") || strings.HasPrefix(input, "钩子:") {
		a.enabled = strings.Contains(input, "开")
		state := "钩子已全部摘除"
		if a.enabled {
			state = "钩子已全部挂载"
		}
		a.lastTrace = append(a.lastTrace, map[string]any{
			"type": "action", "label": state,
			"detail": fmt.Sprintf("当前 %d 条钩子（审计/风控/脱敏），enabled=%v", len(a.hooks), a.enabled)})
		if a.enabled {
			return "✅ 钩子已挂回：" + a.hookList()
		}
		return "⚠️ 钩子已全部摘除 —— 工具现在裸奔，注意对比接下来的输出。"
	}
	if strings.Contains(input, "审计日志") {
		detail := "暂无记录"
		if len(a.auditLog) > 0 {
			detail = strings.Join(a.auditLog, "\n")
		}
		a.lastTrace = append(a.lastTrace, map[string]any{
			"type": "observation", "label": "审计日志（before 钩子写入）", "detail": detail})
		body := "（暂无）"
		if len(a.auditLog) > 0 {
			body = strings.Join(a.auditLog, "\n")
		}
		return "📋 审计日志（before 钩子记录）：\n" + body
	}

	// —— 工具调用指令 → 走钩子管线 ——
	if strings.HasPrefix(input, "查询：") || strings.HasPrefix(input, "查询:") {
		return a.runPipeline("query_user", jw(map[string]any{"name": strings.TrimSpace(string([]rune(input)[3:]))}),
			`{"name": "张三", "phone": "13812345678", "email": "zhangsan@example.com", "level": "VIP"}`)
	}
	if strings.HasPrefix(input, "发送：") || strings.HasPrefix(input, "发送:") {
		payload := strings.TrimSpace(string([]rune(input)[3:]))
		phone := "13800000000"
		if m := mobileFind.FindString(payload); m != "" {
			phone = m
		}
		return a.runPipeline("send_sms", jw(map[string]any{"phone": phone, "message": payload}),
			"短信已提交网关，回执 ok")
	}

	// —— 普通对话 → 模型讲解 ——
	raw, err := llm.SafeChat(a.model, []llm.Message{llm.System(systemPrompt), llm.User(input)})
	if err != nil {
		raw = ""
	}
	answer := raw
	if parsed := llm.TryParse(strings.TrimSpace(raw)); parsed != nil {
		if fin := llm.Str(parsed, "final"); fin != "" {
			answer = fin
		}
	}
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "final", "label": "讲解回答",
		"detail": "试试「查询：张三」「发送：给 13812345678 发 密码是123456」「钩子：关」"})
	return answer
}

// runPipeline 钩子管线：before 链（审计/风控）→ 工具执行 → after 链（脱敏），任何 before 拦截即终止。
func (a *Impl) runPipeline(tool, args, toolResult string) string {
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "action", "label": "工具调用 " + tool, "detail": "参数 " + args})

	// ===== before 钩子链 =====
	payload := args
	for _, h := range a.matchHooks("before", tool) {
		out := h.handler(tool, payload)
		if strings.HasPrefix(out, "DENY:") {
			a.lastTrace = append(a.lastTrace, map[string]any{
				"type": "guard", "label": "before · " + h.name + " → 拦截",
				"detail": strings.TrimPrefix(out, "DENY:") + " —— 钩子在不改工具代码的前提下拦下调用"})
			return "🛑 工具 " + tool + " 被 before 钩子「" + h.name + "」拦截：" + strings.TrimPrefix(out, "DENY:")
		}
		if out != "" {
			a.lastTrace = append(a.lastTrace, map[string]any{
				"type": "thought", "label": "before · " + h.name + " → 改写参数",
				"detail": payload + "  →  " + out})
			payload = out
		} else {
			a.lastTrace = append(a.lastTrace, map[string]any{
				"type": "thought", "label": "before · " + h.name + " → 放行",
				"detail": "未干预（如审计钩子：只记录不改动）"})
		}
	}

	// ===== 工具执行（教学模拟） =====
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "observation", "label": "工具执行完成", "detail": toolResult})

	// ===== after 钩子链（输出合并：按注册顺序依次加工） =====
	output := toolResult
	for _, h := range a.matchHooks("after", tool) {
		if out := h.handler(tool, output); out != "" {
			a.lastTrace = append(a.lastTrace, map[string]any{
				"type": "thought", "label": "after · " + h.name + " → 改写输出",
				"detail": "输出已加工（" + h.name + "）"})
			output = out
		}
	}
	tail := ""
	if a.enabled {
		tail = "\n（before 审计+风控 → 工具 → after 脱敏，按注册顺序依次生效）"
	}
	return "🔗 钩子管线执行完毕：\n最终输出：" + output + tail
}

// matchHooks HookMatcher：按挂载点 + 工具名匹配；enabled=false 时整条链路直通。
func (a *Impl) matchHooks(phase, tool string) []hook {
	if !a.enabled {
		return nil
	}
	var out []hook
	for _, h := range a.hooks {
		if h.phase != phase {
			continue
		}
		for _, m := range h.match {
			if m == "*" || m == tool {
				out = append(out, h)
				break
			}
		}
	}
	return out
}

func (a *Impl) hookList() string {
	var sb strings.Builder
	for _, h := range a.hooks {
		sb.WriteString(fmt.Sprintf("%s·%s(match=[%s]) ", h.phase, h.name, strings.Join(h.match, ", ")))
	}
	return strings.TrimSpace(sb.String())
}

// jw JSON 序列化（标准库实现，等价 Java Json.write）。
func jw(v any) string {
	return llm.Write(v)
}

func (a *Impl) Trace() []map[string]any {
	return agent.CopyTrace(a.lastTrace)
}

func (a *Impl) Reset() {
	a.auditLog = nil
	a.enabled = true
}

var mobileFind = regexp.MustCompile(`1[3-9]\d{9}`)
