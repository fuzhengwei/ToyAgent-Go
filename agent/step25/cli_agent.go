// Step25 · CLI 智能体 —— 把智能体装进终端。
//
// 形态变了，骨架不变：仍然是 agent.Agent.Chat(input)，外面套一层终端会话语义。
//  1. 工具侧换执行器：run_command 直接执行 shell 命令，复用 Step21 的沙箱
//     校验（拒绝名单 → 路径边界 → 命令白名单 → 受限工作区）；
//  2. 工作目录即上下文：受限工作区 sandbox/step25/ 进入系统提示词，
//     像终端会话一样有 cwd；
//  3. 两种交互模式：交互式 REPL（主入口 main.go）与非交互单命令
//     run <stepId> "<message>"（Claude Code 同款 -p 语义）。
//
// 试试本场景的指令：
//
//	运行：ls              —— 沙箱内直接执行，轨迹展示四层防御
//	运行：cat hello.txt   —— 读受限工作区文件
//	运行：rm -rf /        —— 第①层拒绝名单拦截
//	运行：cat /etc/passwd —— 第②层路径边界拦截
//	列出工作区文件        —— 模型自主决策调用 run_command
package step25

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/fuzhengwei/ToyAgent-Go/agent"
	"github.com/fuzhengwei/ToyAgent-Go/llm"
	"github.com/fuzhengwei/ToyAgent-Go/agent/step21"
)

// workspace 受限工作区（相对运行时 CWD）。
const workspace = "sandbox/step25"

// denylist 第①层：拒绝名单（无论是否在沙箱内，一律拒绝）。
var denylist = []string{"rm", "sudo", "shutdown", "reboot", "mkfs", "dd", "kill", "pkill"}

// allowlist 第③层：命令白名单 —— 只放行只读/无害命令。
var allowlist = []string{"ls", "pwd", "echo", "cat", "date", "wc"}

const systemPromptPrefix = `你是一个运行在终端里的 CLI 智能体。当前受限工作区：%s
可以调用 run_command 执行命令（沙箱受四层防御保护）。
`

// Impl CLI 智能体实现。
type Impl struct {
	model     llm.ChatModel
	lastTrace []map[string]any
}

// New 构造 Step25 智能体（初始化受限工作区）。
func New(model llm.ChatModel) agent.Agent {
	a := &Impl{model: model}
	a.initWorkspace()
	return a
}

func (a *Impl) initWorkspace() {
	_ = os.MkdirAll(workspace, 0o755)
	hello := filepath.Join(workspace, "hello.txt")
	if _, err := os.Stat(hello); os.IsNotExist(err) {
		_ = os.WriteFile(hello, []byte("你好，这里是 ToyAgent 的受限工作区。\n"), 0o644)
	}
}

func (a *Impl) Name() string {
	return "Step25 · CLI 智能体（终端 REPL + 沙箱执行器）"
}

func (a *Impl) Chat(input string) string {
	a.lastTrace = nil
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "tool", "label": "终端会话就绪",
		"detail": "工作区 " + workspace + " 已进入系统提示词（cwd 上下文）"})

	// ---- 本地指令：运行：<cmd>（不经模型，直接看沙箱防御） ----
	if strings.HasPrefix(input, "运行：") || strings.HasPrefix(input, "运行:") {
		return a.execute(strings.TrimSpace(string([]rune(input)[3:])))
	}

	// ---- 常规对话：模型自主决策是否调用 run_command ----
	messages := []llm.Message{
		llm.System(fmt.Sprintf(systemPromptPrefix, workspace) +
			"\n决策规则：\n- 需要执行命令时，只输出 JSON：{\"tool\": \"run_command\", \"arguments\": {\"command\": \"命令\"}}\n" +
			"- 不需要命令时，直接用自然语言回答，不要输出 JSON。"),
		llm.User(input),
	}

	for round := 1; round <= 2; round++ {
		raw, err := llm.SafeChat(a.model, messages)
		if err != nil {
			raw = err.Error()
		}
		decision := llm.TryParse(strings.TrimSpace(raw))

		if decision != nil && llm.Str(decision, "tool") != "" {
			cmd := ""
			if argsMap, ok := decision["arguments"].(map[string]any); ok && argsMap["command"] != nil {
				cmd = fmt.Sprintf("%v", argsMap["command"])
			}
			a.lastTrace = append(a.lastTrace, map[string]any{
				"type": "action", "label": "模型发起命令执行", "detail": cmd})
			result := a.runGuarded(cmd)
			messages = append(messages, llm.Assistant(raw))
			messages = append(messages, llm.User("工具 run_command 执行结果: "+result))
			continue
		}
		a.lastTrace = append(a.lastTrace, map[string]any{
			"type": "final", "label": "最终回答",
			"detail": "CLI 形态：模型基于终端会话上下文完成回答"})
		return raw
	}
	return "已达最大轮数，循环终止。"
}

// execute 本地指令入口：同样走四层防御，但直接返回结果。
func (a *Impl) execute(cmd string) string {
	result := a.runGuarded(cmd)
	return "命令：" + cmd + "\n结果：\n" + result
}

// runGuarded 四层防御：拒绝名单 → 路径边界 → 命令白名单 → 复用 Step21 校验 + 受限工作区执行。
//
// 与 Step21 的关系：①②③层是 CLI 形态自有的防御（与 Java 版一致），
// 白名单放行后再调用 step21.CheckLayers 复用其黑名单/空间边界/路径规范化
// 三重校验作为纵深兜底（规范要求的跨场景复用），最后才在工作区内真实执行。
func (a *Impl) runGuarded(cmd string) string {
	if strings.TrimSpace(cmd) == "" {
		return "（空命令）"
	}

	// ① 拒绝名单：整个命令串里出现即拦截
	lower := strings.ToLower(cmd)
	for _, bad := range denylist {
		if strings.Contains(lower, bad) {
			a.lastTrace = append(a.lastTrace, map[string]any{
				"type": "guard", "label": "第①层 · 拒绝名单",
				"detail": fmt.Sprintf("命中 %s → 直接拒绝（高危命令无商量余地）", bad)})
			return "已拦截：命令包含拒绝名单中的 " + bad + "。"
		}
	}
	// ② 路径边界：.. 或绝对路径一律拒绝
	for _, token := range strings.Fields(cmd) {
		if strings.Contains(token, "..") || strings.HasPrefix(token, "/") {
			a.lastTrace = append(a.lastTrace, map[string]any{
				"type": "guard", "label": "第②层 · 路径边界",
				"detail": "token " + token + " 越出受限工作区 → 拒绝"})
			return "已拦截：路径 " + token + " 越出受限工作区 " + workspace + "。"
		}
	}
	// ③ 命令白名单：只放行只读/无害命令
	head := strings.Fields(cmd)[0]
	allowed := false
	for _, w := range allowlist {
		if head == w {
			allowed = true
			break
		}
	}
	if !allowed {
		a.lastTrace = append(a.lastTrace, map[string]any{
			"type": "guard", "label": "第③层 · 命令白名单",
			"detail": fmt.Sprintf("%s 不在白名单 [%s] 中 → 拒绝", head, strings.Join(allowlist, ", "))})
		return "已拦截：命令 " + head + " 不在白名单中（教学沙箱只放行 [" + strings.Join(allowlist, ", ") + "]）。"
	}

	// ③½ 复用 Step21 沙箱校验（黑名单 / 空间边界 / 路径规范化，纵深防御兜底）
	workspaceAbs, _ := filepath.Abs(workspace)
	if v := step21.CheckLayers(cmd, workspaceAbs); !v.Allowed {
		a.lastTrace = append(a.lastTrace, map[string]any{
			"type": "guard", "label": fmt.Sprintf("复用 Step21 · 第 %d 层拦截", v.Layer),
			"detail": v.Reason + " —— step25 的 shell 执行器复用 step21 沙箱防御"})
		return "已拦截：" + v.Reason + "。"
	}

	// ④ 受限工作区：指定工作目录执行，命令按空格拆分（不经 shell，无管道/重定向）
	fields := strings.Fields(cmd)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmdExec := exec.CommandContext(ctx, fields[0], fields[1:]...)
	cmdExec.Dir = workspace
	out, err := cmdExec.CombinedOutput()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "执行失败: 命令超时（5 秒上限）"
		}
		if _, lookErr := exec.LookPath(fields[0]); lookErr != nil {
			return "执行失败: " + err.Error()
		}
		// 命令存在但退出码非 0：Java 版仍取输出（redirectErrorStream 合并）
		result := strings.TrimSpace(string(out))
		a.lastTrace = append(a.lastTrace, map[string]any{
			"type": "observation", "label": "沙箱执行 " + cmd,
			"detail": fmt.Sprintf("cwd=%s，退出码非 0", workspace)})
		if result == "" {
			return "（无输出）"
		}
		return result
	}
	result := strings.TrimSpace(string(out))
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "observation", "label": "沙箱执行 " + cmd,
		"detail": fmt.Sprintf("cwd=%s，退出码 0", workspace)})
	if result == "" {
		return "（无输出）"
	}
	return result
}

func (a *Impl) Trace() []map[string]any {
	return agent.CopyTrace(a.lastTrace)
}

func (a *Impl) Reset() {
	a.initWorkspace()
}
