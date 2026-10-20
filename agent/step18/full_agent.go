// Step18 · 全流程智能体 —— 把前 17 个场景的能力串成一条完整链路。
//
// 前面的场景每次只演示一种能力，这一个包把它们全部组装到一起，
// 一次 Chat() 完整走完生产级智能体的标准生命周期：
//
//	输入守卫 → 读取记忆(画像+历史) → 组装上下文 → ReAct 循环(思考→工具→观察)
//	       → 保险丝 → 输出校验 → 回写记忆
//
// 注意：它依然只是一个场景包 + 一个 Chat(input) 方法 ——
// 智能体再复杂，骨架不变；变厚的只是「策略层」。
// 这也是理解生产框架（LangGraph / Dify / 工业级 Harness）的钥匙：
// 框架只是把这些环节做成了可配置、可观测、可容错的工程件。
package step18

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/fuzhengwei/ToyAgent-Go/agent"
	"github.com/fuzhengwei/ToyAgent-Go/llm"
	"github.com/fuzhengwei/ToyAgent-Go/agent/step03"
)

const (
	// maxSteps 循环保险丝：最多工具轮数（Budget Guard 的一种）。
	maxSteps = 5
	// maxInputLen 输入长度守卫阈值。
	maxInputLen = 300
	// compressThreshold 记忆窗口压缩阈值：历史超过该条数时触发压缩。
	compressThreshold = 10
)

// systemPrompt 系统提示词：角色 + 能力清单 + 输出协议（Step02 的三层结构）。
const systemPrompt = `你是 ToyAgent 综合智能体，具备记忆与工具使用能力。
每一轮只输出一个 JSON 对象，格式二选一：
1. 继续行动：{"thought": "你的思考", "action": "工具名", "action_input": "参数"}
2. 给出答案：{"thought": "你的思考", "final": "最终答案"}
可用工具：
- get_weather(city): 查询城市天气
- calculator(expression): 计算四则运算，如 12*8
- get_time(): 获取当前时间
回答时自然融入用户画像与历史信息；不要输出 JSON 以外的内容。
`

var (
	// sensitive 敏感意图黑名单（教学演示，生产环境用内容安全服务）。
	sensitive = regexp.MustCompile(`(?i)hack|攻击|爆破|破解|密码`)
	// namePtn 画像提取：记住「我叫 XX」（排除「我叫什么」这类疑问句）。
	// Java 用负向前瞻 (?!)，Go RE2 不支持 —— 改为匹配后跳过被否决的捕获组，
	// 行为等价（见 findName）。
	namePtn = regexp.MustCompile(`我(?:叫|的名字(?:是|叫)?)\s*([\x{4e00}-\x{9fa5}A-Za-z0-9]{1,12})`)
	// likePtn 画像提取：记住「我喜欢 XX」。
	likePtn = regexp.MustCompile(`我(?:喜欢|爱)\s*([\x{4e00}-\x{9fa5}A-Za-z0-9·\s]{1,12})`)
	// calcRe 对齐 Java matches() 全串匹配语义。
	calcRe = regexp.MustCompile(`^\s*(\d+(?:\.\d+)?)\s*([+\-*/])\s*(\d+(?:\.\d+)?)\s*$`)
)

// Impl 全流程智能体实现：跨轮记忆（历史消息与用户画像）是实例字段。
type Impl struct {
	model     llm.ChatModel
	lastTrace []map[string]any
	history   []llm.Message
	profile   map[string]string // 画像：名字 / 喜好（保持固定顺序输出）
}

// New 构造 Step18 智能体。
func New(model llm.ChatModel) agent.Agent {
	return &Impl{model: model, profile: map[string]string{}}
}

func (a *Impl) Name() string {
	return "Step18 · 全流程智能体（守卫+记忆+ReAct+工具+保险丝）"
}

func (a *Impl) Chat(input string) string {
	a.lastTrace = nil

	// ---------- 1. 输入守卫（Step15 的 Guard） ----------
	if len([]rune(input)) > maxInputLen {
		a.lastTrace = append(a.lastTrace, map[string]any{
			"type": "guard", "label": "输入守卫",
			"detail": fmt.Sprintf("输入长度 %d 超过上限 %d，直接拒绝，不进模型。", len([]rune(input)), maxInputLen)})
		return fmt.Sprintf("输入过长，请精简后重试（守卫规则：长度 ≤ %d 字符）。", maxInputLen)
	}
	if sm := sensitive.FindString(input); sm != "" {
		a.lastTrace = append(a.lastTrace, map[string]any{
			"type": "guard", "label": "输入守卫",
			"detail": fmt.Sprintf("命中敏感意图黑名单「%s」，拦截且不消耗模型预算。", sm)})
		return "该请求涉及不安全操作，已被输入守卫拦截。智能体的第一道防线在模型之前。"
	}
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "guard", "label": "输入守卫",
		"detail": "长度与安全检查通过，放行进入后续流程。"})

	// ---------- 2. 记忆读取 + 画像提取（Step05 的 Memory） ----------
	a.extractProfile(input)
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "memory", "label": "读取记忆",
		"detail": fmt.Sprintf("历史 %d 条；画像：%s（随请求一起组装进上下文）。",
			len(a.history), a.profileString())})

	// ---------- 3. 组装上下文（system + 历史 + 本轮输入） ----------
	messages := []llm.Message{llm.System(systemPrompt + a.userPortrait())}
	messages = append(messages, a.history...)
	messages = append(messages, llm.User(input))

	// ---------- 4. ReAct 循环（Step03/04 的 Thought→Action→Observation） ----------
	answer := a.runReAct(messages)

	// ---------- 5. 输出校验（守卫不只管输入） ----------
	answer = a.outputGuard(answer, input)

	// ---------- 6. 记忆回写 ----------
	a.history = append(a.history, llm.User(input))
	a.history = append(a.history, llm.Assistant(answer))
	memo := fmt.Sprintf("本轮对话已写入记忆（现共 %d 条）。", len(a.history))
	if len(a.history) > compressThreshold {
		a.compressHistory()
		memo += fmt.Sprintf(" 超过阈值 %d 条 → 上下文压缩已执行（保留系统提示词与最近消息）。", compressThreshold)
	}
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "memory", "label": "回写记忆", "detail": memo})
	return answer
}

// runReAct ReAct 主体：与 Step03 相同的循环骨架，外层多了守卫与记忆。
func (a *Impl) runReAct(messages []llm.Message) string {
	for step := 1; step <= maxSteps; step++ {
		raw, err := llm.SafeChat(a.model, messages)
		if err != nil {
			a.lastTrace = append(a.lastTrace, map[string]any{
				"type": "final", "label": "Final Answer", "detail": err.Error()})
			return err.Error()
		}
		raw = strings.TrimSpace(raw)
		parsed := llm.TryParse(raw)
		if parsed == nil {
			// 模型没按协议输出 → 当作最终答案兜底返回
			a.lastTrace = append(a.lastTrace, map[string]any{
				"type": "final", "label": "Final Answer", "detail": raw})
			return raw
		}

		a.lastTrace = append(a.lastTrace, map[string]any{
			"type": "thought", "label": fmt.Sprintf("Thought %d", step),
			"detail": llm.Str(parsed, "thought")})

		if fin := llm.Str(parsed, "final"); fin != "" {
			a.lastTrace = append(a.lastTrace, map[string]any{
				"type": "final", "label": "Final Answer", "detail": fin})
			return fin
		}

		action := llm.Str(parsed, "action")
		actionInput := llm.Str(parsed, "action_input")
		a.lastTrace = append(a.lastTrace, map[string]any{
			"type": "action", "label": fmt.Sprintf("Action %d", step),
			"detail": action + "(" + actionInput + ")"})

		observation := a.executeTool(action, actionInput)
		a.lastTrace = append(a.lastTrace, map[string]any{
			"type": "tool", "label": "Tool · " + action, "detail": "工具执行完成，产出观察结果。"})
		a.lastTrace = append(a.lastTrace, map[string]any{
			"type": "observation", "label": fmt.Sprintf("Observation %d", step), "detail": observation})

		messages = append(messages, llm.Assistant(raw))
		messages = append(messages, llm.User("Observation: "+observation))
	}
	msg := fmt.Sprintf("已达最大步数 %d，保险丝熔断，循环终止（Budget Guard）。", maxSteps)
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "guard", "label": "保险丝", "detail": msg})
	return msg
}

// executeTool 工具注册表：本地 switch 执行器（真实项目里是 MCP/插件网关）。
func (a *Impl) executeTool(action, input string) (result string) {
	defer func() {
		if r := recover(); r != nil {
			result = fmt.Sprintf("工具执行失败: %v", r)
		}
	}()
	switch action {
	case "get_weather":
		return step03.MockWeather(input)
	case "calculator":
		v, err := calc(input)
		if err != nil {
			return "工具执行失败: " + err.Error()
		}
		return llm.FormatDouble(v)
	case "get_time":
		return time.Now().Format("2006-01-02 15:04:05")
	default:
		if action == "" {
			return "未知工具: "
		}
		return "未知工具: " + action
	}
}

// outputGuard 输出守卫：拦截答案中的敏感内容，并在能回答画像问题时兜底。
func (a *Impl) outputGuard(answer, input string) string {
	if answer != "" && sensitive.MatchString(answer) {
		a.lastTrace = append(a.lastTrace, map[string]any{
			"type": "guard", "label": "输出守卫",
			"detail": "答案命中敏感词，替换为安全话术后返回。"})
		return "该内容不宜展示，已由输出守卫处理。"
	}
	// 记忆兜底：模型看不到被压缩掉的历史时，画像仍能回答「我叫什么」
	if strings.Contains(input, "我叫什么") || strings.Contains(input, "我是谁") {
		if name, ok := a.profile["名字"]; ok {
			a.lastTrace = append(a.lastTrace, map[string]any{
				"type": "memory", "label": "画像兜底",
				"detail": fmt.Sprintf("命中画像「名字=%s」，无需再问模型。", name)})
			return "你叫 " + name + "（来自画像记忆，不依赖模型）。"
		}
	}
	return answer
}

// extractProfile 从输入中提取用户画像（生产环境由记忆组件用模型抽取）。
func (a *Impl) extractProfile(input string) {
	if name := findName(input); name != "" {
		a.profile["名字"] = name
	}
	if m := likePtn.FindStringSubmatch(input); m != nil {
		a.profile["喜好"] = strings.TrimSpace(m[1])
	}
}

// findName 等价 Java NAME_PTN 的负向前瞻 (?!(?:什么|啥|谁))：
// 跳过捕获组以疑问词开头的匹配，继续找下一个。
func findName(input string) string {
	for _, m := range namePtn.FindAllStringSubmatch(input, -1) {
		name := m[1]
		if strings.HasPrefix(name, "什么") || strings.HasPrefix(name, "啥") || strings.HasPrefix(name, "谁") {
			continue
		}
		return name
	}
	return ""
}

// profileString 画像拼接（对齐 Java Map.toString 的 {k=v} 形式；顺序固定：名字 → 喜好）。
func (a *Impl) profileString() string {
	if len(a.profile) == 0 {
		return ""
	}
	var parts []string
	if v, ok := a.profile["名字"]; ok {
		parts = append(parts, "名字="+v)
	}
	if v, ok := a.profile["喜好"]; ok {
		parts = append(parts, "喜好="+v)
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

// userPortrait 画像注入提示词的片段。
func (a *Impl) userPortrait() string {
	if len(a.profile) == 0 {
		return ""
	}
	return "\n已知用户画像：" + a.profileString() + "（回答时自然使用，不要逐字复述）。"
}

// compressHistory 上下文压缩：保留系统提示词语义（此实现重建时自动携带）与最近 6 条。
func (a *Impl) compressHistory() {
	keep := 6
	if len(a.history) < keep {
		keep = len(a.history)
	}
	kept := append([]llm.Message(nil), a.history[len(a.history)-keep:]...)
	a.history = kept
}

// calc 教学用四则运算求值（与 Step03 相同）；解析失败返回 error。
func calc(expr string) (float64, error) {
	m := calcRe.FindStringSubmatch(expr)
	if m == nil {
		return 0, fmt.Errorf("无法解析表达式: %s", expr)
	}
	aNum, err1 := strconv.ParseFloat(m[1], 64)
	bNum, err2 := strconv.ParseFloat(m[3], 64)
	if err1 != nil || err2 != nil {
		return 0, fmt.Errorf("无法解析表达式: %s", expr)
	}
	switch m[2] {
	case "+":
		return aNum + bNum, nil
	case "-":
		return aNum - bNum, nil
	case "*":
		return aNum * bNum, nil
	case "/":
		if bNum == 0 {
			return 0, fmt.Errorf("除数不能为 0")
		}
		return aNum / bNum, nil
	default:
		return 0, fmt.Errorf("不支持的运算符 %s", m[2])
	}
}

func (a *Impl) Trace() []map[string]any {
	return agent.CopyTrace(a.lastTrace)
}

func (a *Impl) Reset() {
	a.history = nil
	a.profile = map[string]string{}
}
