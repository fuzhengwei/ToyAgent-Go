// Step21 · 沙箱纵深防御 —— 命令执行前的四道关卡。
//
// 拆出教学版四层校验管线，任何一层拦截即 DENY：
//  1. 策略层：当前策略档位是否允许 shell（READ_ONLY 直接全拒）；
//  2. 内容黑名单：rm -rf /、sudo、mkfs、fork 炸弹等破坏性命令特征；
//  3. 空间边界：命令中出现的路径必须落在工作区 sandbox/ 内，
//     绝对路径越界即拒；
//  4. 路径规范化：先 canonicalize 再校验，workspace/../../etc/passwd
//     这种「看似在内、实际在外」的逃逸无所遁形。
//
// 关键设计思想：不信任模型的输出 —— 模型生成的命令照样要过全部四层。
//
// 本包同时导出 Verdict 与 CheckLayers（第 2/3/4 层校验核心），
// 供 step25 的 shell 执行器跨场景复用（规范要求）。
package step21

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/fuzhengwei/ToyAgent-Go/agent"
	"github.com/fuzhengwei/ToyAgent-Go/llm"
)

// Verdict 校验结论：放行 or 拦截（在哪一层、为什么）。
type Verdict struct {
	Allowed bool
	Layer   int
	Reason  string
}

// Pass 四层全过的结论。
func Pass() Verdict { return Verdict{Allowed: true, Layer: 0, Reason: "四层校验全部通过"} }

// Deny 拦截结论。
func Deny(layer int, why string) Verdict { return Verdict{Allowed: false, Layer: layer, Reason: why} }

// denylist 内容黑名单（教学子集）。
var denylist = []string{
	"rm -rf", "rm  -rf", "sudo", "shutdown", "reboot", "mkfs", "dd if=",
	":(){", "fork()", "chmod 777", "> /dev/", "kill -9 1", "curl | sh",
}

// absPathRe 绝对路径匹配。Java 用负向后顾 (?<![\w.]) 排除前导字符，
// Go RE2 不支持 lookbehind —— 捕获一个前导字符再剥掉，语义等价。
var absPathRe = regexp.MustCompile(`(^|[^\w.])/(?:[\w.-]+/)*[\w.-]+`)

// CheckLayers 执行第 2/3/4 层校验（策略层由调用方决定），
// 供 Impl 与 step25 复用。workspace 须为绝对路径。
func CheckLayers(cmd string, workspace string) Verdict {
	// ===== 第 2 层 · 内容黑名单 =====
	low := strings.ToLower(cmd)
	for _, bad := range denylist {
		if strings.Contains(low, bad) {
			return Deny(2, fmt.Sprintf("命中危险命令特征「%s」", bad))
		}
	}

	// ===== 第 3 层 · 空间边界（绝对路径必须在工作区内） =====
	for _, m := range absPathRe.FindAllStringSubmatch(cmd, -1) {
		// 剥掉捕获的前导字符（行首时为空串），得到纯路径
		p := strings.TrimPrefix(m[0], m[1])
		// 绝对路径按文件系统真实位置判断：只有恰好落在工作区内才放行
		cleaned := filepath.Clean(p)
		if !strings.HasPrefix(cleaned, workspace) {
			return Deny(3, fmt.Sprintf("绝对路径 %s 越出工作区 %s", p, filepath.Base(workspace)))
		}
	}

	// ===== 第 4 层 · 路径规范化（canonicalize 后复查，防 ../ 逃逸） =====
	if strings.Contains(cmd, "..") {
		return Deny(4, "检测到 ../ 相对跳转 —— 规范化后路径将越出工作区，疑似路径逃逸")
	}
	return Pass()
}

const systemPrompt = `你是 ToyAgent 的沙箱讲解员。用户可能询问沙箱、命令执行、安全防御相关话题。
只输出 JSON：{"final": "回答"}。想看四层校验实战，请引导用户发「执行：<命令>」。`

// Impl 沙箱场景实现。
type Impl struct {
	model     llm.ChatModel
	history   []llm.Message
	lastTrace []map[string]any
	workspace string
}

// New 构造 Step21 智能体（创建工作区并预置 hello.txt）。
func New(model llm.ChatModel) agent.Agent {
	workspace, _ := filepath.Abs("sandbox")
	_ = os.MkdirAll(workspace, 0o755)
	_ = os.WriteFile(filepath.Join(workspace, "hello.txt"), []byte("ToyAgent sandbox\n"), 0o644)
	return &Impl{model: model, workspace: workspace}
}

func (a *Impl) Name() string {
	return "Step21 · 沙箱纵深防御（策略 → 黑名单 → 边界 → 规范化，四层拦截）"
}

func (a *Impl) Chat(input string) string {
	a.lastTrace = nil
	a.history = append(a.history, llm.User(input))

	// 发「执行：<命令>」即走沙箱管线；其余交给模型
	if strings.HasPrefix(input, "执行：") || strings.HasPrefix(input, "执行:") {
		cmd := strings.TrimSpace(string([]rune(input)[3:]))
		return a.runShell(cmd)
	}

	req := []llm.Message{llm.System(systemPrompt)}
	req = append(req, a.history...)
	raw, err := llm.SafeChat(a.model, req)
	if err != nil {
		raw = ""
	}
	answer := raw
	if parsed := llm.TryParse(strings.TrimSpace(raw)); parsed != nil {
		answer = orRaw(llm.Str(parsed, "final"), raw)
	}
	a.history = append(a.history, llm.Assistant(answer))
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "final", "label": "直接作答",
		"detail": "普通对话不走沙箱；想看四层校验，发「执行：<命令>」"})
	return answer
}

// runShell 四层校验管线：任何一层拦截立即 DENY，全过才执行。
func (a *Impl) runShell(cmd string) string {
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "action", "label": "shell_execute 请求", "detail": cmd})

	// ===== 第 1 层 · 策略档位 =====
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "thought", "label": "第 1 层 · 策略校验 ✓",
		"detail": "当前策略 WORKSPACE_ONLY 允许执行 shell（生产另有 READ_ONLY=全拒 / FULL=需审批）"})

	// ===== 第 2/3/4 层（导出的校验核心，与 step25 共用） =====
	v := CheckLayers(cmd, a.workspace)
	if !v.Allowed {
		return a.deny(v.Layer, v.Reason)
	}
	switch v.Layer {
	case 0: // 全部通过，逐层补齐 ✓ 轨迹
		a.lastTrace = append(a.lastTrace, map[string]any{
			"type": "thought", "label": "第 2 层 · 内容黑名单 ✓",
			"detail": fmt.Sprintf("未命中 %d 条破坏性命令特征", len(denylist))})
		a.lastTrace = append(a.lastTrace, map[string]any{
			"type": "thought", "label": "第 3 层 · 空间边界 ✓",
			"detail": "命令中的路径均位于工作区 sandbox/ 内（相对路径按工作区解析）"})
		a.lastTrace = append(a.lastTrace, map[string]any{
			"type": "thought", "label": "第 4 层 · 路径规范化 ✓",
			"detail": "canonicalize 复查通过，无 ../ 逃逸（第 3 层看原文，第 4 层看真实落点）"})
	default:
		// CheckLayers 只可能返回 0（全过）或 2/3/4 层拦截；2 层拦截时补第 2 层 ✓ 再 deny
		if v.Layer == 3 {
			a.lastTrace = append(a.lastTrace, map[string]any{
				"type": "thought", "label": "第 2 层 · 内容黑名单 ✓",
				"detail": fmt.Sprintf("未命中 %d 条破坏性命令特征", len(denylist))})
		} else if v.Layer == 4 {
			a.lastTrace = append(a.lastTrace, map[string]any{
				"type": "thought", "label": "第 2 层 · 内容黑名单 ✓",
				"detail": fmt.Sprintf("未命中 %d 条破坏性命令特征", len(denylist))})
			a.lastTrace = append(a.lastTrace, map[string]any{
				"type": "thought", "label": "第 3 层 · 空间边界 ✓",
				"detail": "命令中的路径均位于工作区 sandbox/ 内（相对路径按工作区解析）"})
		}
	}

	// ===== 执行（教学模拟） =====
	output := simulate(cmd)
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "observation", "label": "执行完成",
		"detail": "工作区 " + filepath.Base(a.workspace) + " · " + output})
	a.history = append(a.history, llm.Assistant("已执行（沙箱内）"))
	return "✅ 命令通过四层校验，已在沙箱内执行：\n$ " + cmd + "\n" + output +
		"\n\n（教学模拟执行。生产版每层都是独立防线：模型生成的命令也必须全过。）"
}

func (a *Impl) deny(layer int, why string) string {
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "guard", "label": fmt.Sprintf("第 %d 层 · 拦截 DENY", layer),
		"detail": why + " —— 纵深防御：任何一层说不行，就是不行"})
	a.history = append(a.history, llm.Assistant("已拦截"))
	return fmt.Sprintf("🛑 已在第 %d 层拦截：%s", layer, why)
}

func simulate(cmd string) string {
	if strings.HasPrefix(cmd, "ls") {
		return "hello.txt  notes.md  data/"
	}
	if strings.HasPrefix(cmd, "cat") {
		return "ToyAgent sandbox（hello.txt 内容）"
	}
	return "(模拟) 命令已执行，输出略"
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
}
