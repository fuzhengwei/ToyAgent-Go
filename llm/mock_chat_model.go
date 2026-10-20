package llm

import (
	"fmt"
	"regexp"
	"strings"
)

// MockChatModel 教学演示用 Mock 模型：不联网、不花钱，让 26 个场景开箱即跑。
//
// 它并非"真智能"，而是按场景预设了符合协议的返回（例如工具调用时
// 返回 JSON 决策、ReAct 时返回 Thought/Action），让学习者可以完整
// 观察智能体的「骨架」如何运转。配置真实 API Key 后骨架不变，
// 换上真大脑即可。
//
// 本文件逐场景 1:1 移植自 Java 版 MockChatModel（剧本、正则、关键词、
// 返回文案均保持一致）。
type MockChatModel struct {
	scenario string
}

// NewMockChatModel 按场景构造 Mock 模型。
func NewMockChatModel(scenario string) *MockChatModel {
	return &MockChatModel{scenario: scenario}
}

// Chat 按场景剧本返回预设响应。
func (m *MockChatModel) Chat(messages []Message) string {
	system := ""
	if len(messages) > 0 {
		system = messages[0].Content
	}
	last := lastUserText(messages)
	var sb strings.Builder
	for _, msg := range messages {
		sb.WriteString("\n")
		sb.WriteString(msg.Content)
	}
	all := sb.String()

	switch m.scenario {
	case "step01":
		return mockChat(last)
	case "step02":
		return mockPrompt(last, system)
	case "step03":
		return mockReAct(all, last)
	case "step04":
		return mockToolCall(last)
	case "step05":
		return mockMemory(all, last)
	case "step06":
		return mockRouter(last)
	case "step09":
		return mockMcp(last)
	case "step10":
		return mockSkill(all, last)
	case "step07":
		return mockRag(all, last)
	case "step12":
		return mockMulti(all, system)
	case "step15":
		return mockLoop(last, all)
	case "step16":
		return mockWorkflow(all, last)
	case "step18":
		return mockFull(all, last)
	case "step11":
		return mockRegistry(last)
	case "step17":
		return mockRuntime(all, last)
	case "step19":
		return mockAsk(all, last)
	case "step20":
		return mockApproval(last)
	case "step21":
		return mockSandbox(last)
	case "step22":
		return mockEvent(all, last)
	case "step23":
		return mockPlugin(all, last)
	case "step25":
		return mockCli(all, last, system)
	case "step13":
		return mockSubagent(all, last, system)
	case "step24":
		return mockHooks(all, last, system)
	case "step14":
		return mockA2a(all, last, system)
	case "step26":
		return mockSchedule(all, last, system)
	default:
		return "（Mock 模型）收到：" + last
	}
}

func lastUserText(messages []Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			return messages[i].Content
		}
	}
	return ""
}

// ------------------------------------------------------------- step01 对话

func mockChat(input string) string {
	if strings.Contains(input, "你好") || strings.Contains(input, "hi") || strings.Contains(input, "hello") {
		return "你好！我是 ToyAgent 里的最小智能体。我的全部实现只有一个方法：chat(input)。输入进来，模型处理，回答出去 —— 这就是智能体的最小 MVP。"
	}
	return "（Mock 模型）已收到你的消息：「" + clip(input) + "」。\n" +
		"Step01 的实现没有策略、没有工具、没有记忆 —— 每次对话都是一次独立的「输入 → 模型 → 输出」。\n" +
		"配置真实 API Key 后，这句回复将由真实大模型生成。继续试试右边的示例问题吧。"
}

// ------------------------------------------------------------- step02 提示词

func mockPrompt(input, system string) string {
	oneLine := strings.Contains(input, "一句话")
	if oneLine {
		return "好的，遵守「一句话」约束：智能体就是一个用大模型做决策、用工具做执行的程序。"
	}
	return "好的，我是你的技术助理（角色由系统提示词设定）。「" + clip(input) + "」可以这样理解：\n\n" +
		"1. 定义 —— 智能体 = 大模型(大脑) + 工具(手脚) + 循环(节拍)。\n" +
		"2. 原理 —— 模型根据系统提示词中的人设与约束来组织回答，本条回复就是模板化输出的效果。\n" +
		"3. 建议 —— 好提示词 = 明确角色 + 交代背景 + 给出任务与约束，这正是 Step02 代码里 systemPrompt 的三层结构。\n\n" +
		"（Mock 模型按系统提示词的格式约定作答；真实模型会表现出同样的角色倾向。）"
}

// ------------------------------------------------------------- step18 全流程

// mockFull 与 step03 同一套 ReAct JSON 协议，但画像信息会让回答更「有记忆」。
func mockFull(all, last string) string {
	portrait := ""
	if strings.Contains(all, "名字={") {
		portrait = "，" + cutAtBrace(extractAfter(all, "名字={")) + "同学"
	} else if strings.Contains(all, "喜好={") {
		portrait = "，还记得你喜欢" + cutAtBrace(extractAfter(all, "喜好={"))
	}
	if strings.Contains(all, "Observation:") {
		obs := extractAfter(all, "Observation:")
		tail := "。"
		if portrait != "" {
			tail = "（" + strings.TrimPrefix(portrait, "，") + "）。"
		}
		return jw(map[string]any{
			"thought": "工具结果已返回，结合记忆与观察结果组织最终回答",
			"final":   clip(obs) + " —— 本轮完整走过了：守卫 → 记忆 → 循环 → 工具 → 输出校验 → 回写记忆" + tail,
		})
	}
	if exprRe.MatchString(last) {
		expr := "1+1"
		if m := exprFind.FindStringSubmatch(last); m != nil {
			expr = m[1]
		}
		return jw(map[string]any{
			"thought":      "有算式，调用计算器工具，不能靠心算",
			"action":       "calculator",
			"action_input": expr,
		})
	}
	city := findCity(last)
	if city != "北京" || strings.Contains(last, "天气") {
		return jw(map[string]any{
			"thought":      "问题需要实时数据，先调天气工具",
			"action":       "get_weather",
			"action_input": city,
		})
	}
	greet := "你好！"
	if portrait != "" {
		greet = "你好" + portrait + "！"
	}
	profile := "还没有你的档案"
	if portrait != "" {
		profile = "有你的画像"
	}
	return jw(map[string]any{
		"thought": "普通寒暄无需工具，直接结合记忆回答",
		"final": greet +
			"我是全流程智能体：守卫已放行你的输入，记忆里" + profile +
			"，本轮无需调用工具即可作答。",
	})
}

// cutAtBrace 等价 Java 的 replaceFirst("\\}.*", "")：截掉第一个 } 及其后内容。
func cutAtBrace(s string) string {
	if i := strings.Index(s, "}"); i >= 0 {
		return s[:i]
	}
	return s
}

// ------------------------------------------------------------- step03 ReAct

func mockReAct(all, last string) string {
	if strings.Contains(all, "Observation:") {
		obs := extractAfter(all, "Observation:")
		return jw(map[string]any{
			"thought": "已拿到工具观察结果，可以回答用户了",
			"final":   "根据工具查询结果：" + clip(obs) + " —— 这就是 ReAct：先想（Thought）、再做（Action）、看结果（Observation），直到能给出最终答案（Final Answer）。",
		})
	}
	if exprRe.MatchString(last) {
		expr := "1+1"
		if m := exprFind.FindStringSubmatch(last); m != nil {
			expr = m[1]
		}
		return jw(map[string]any{
			"thought":      "这是一个算式，我不能心算，应该调用计算器工具保证准确",
			"action":       "calculator",
			"action_input": expr,
		})
	}
	city := findCity(last)
	return jw(map[string]any{
		"thought":      "用户询问了天气相关的问题，我需要先调用天气工具获取实时数据",
		"action":       "get_weather",
		"action_input": city,
	})
}

// ------------------------------------------------------------- step04 工具调用

func mockToolCall(last string) string {
	// 工具结果回填轮：不再发起调用，基于事实生成回答
	if strings.Contains(last, "执行结果") || strings.Contains(last, "Observation") {
		fact := extractAfter(last, "执行结果")
		fallback := clip(last)
		if strings.TrimSpace(fact) != "" {
			fallback = clip(fact)
		}
		return "根据工具返回的结果：" + fallback + "，这就是你要的答案 —— 工具调用链闭环完成。"
	}
	if strings.Contains(last, "天气") || strings.Contains(last, "weather") {
		return jw(map[string]any{
			"tool":      "get_weather",
			"arguments": map[string]any{"city": findCity(last)},
		})
	}
	if m := exprFind.FindStringSubmatch(last); m != nil {
		return jw(map[string]any{
			"tool":      "calculator",
			"arguments": map[string]any{"expression": m[1]},
		})
	}
	return "这个问题不需要调用工具，我直接回答：「" + clip(last) + "」—— 工具调用的关键在于：模型自己判断「要不要用工具、用哪个、参数是什么」。"
}

// ------------------------------------------------------------- step05 记忆

func mockMemory(all, last string) string {
	// 从完整对话历史里找"我叫X"，真实体现"记忆 = 把历史放进上下文"
	// 注：Java 版此处未过滤「我叫什么」这类疑问句（导致问句被当成名字），
	// 与验收标准「两轮能记住名字」冲突；参照 step22 剧本对疑问词做跳过，
	// 这是 Go 版对 Java 行为的一处有意修正（详见汇报）。
	remembered := ""
	for _, m := range nameFind.FindAllStringSubmatch(all, -1) {
		n := m[1]
		if strings.HasPrefix(n, "什么") || strings.HasPrefix(n, "名字") || strings.HasPrefix(n, "谁") || strings.HasPrefix(n, "啥") {
			continue
		}
		remembered = n
	}

	if strings.Contains(last, "我叫") && !nameQuestionRe.MatchString(last) {
		return "你好，" + remembered + "！我已把你的名字放进对话历史（注意看右侧的「记忆窗口」计数）。"
	}
	if strings.Contains(last, "叫什么") || strings.Contains(last, "记得") || strings.Contains(last, "谁") {
		if remembered != "" {
			return "当然记得，你叫「" + remembered + "」。我没有数据库 —— 是因为之前那轮对话还在上下文窗口里，这就是短期记忆的全部秘密。"
		}
		return "目前的历史里还没有你的名字，先告诉我「我叫XX」吧。"
	}
	if strings.Contains(last, "压缩") || strings.Contains(last, "总结") {
		return "已对历史对话做了压缩摘要：保留关键事实（如你的名字、讨论主题），丢弃寒暄与重复内容 —— 滑窗 + 摘要，就是上下文工程最常用的两板斧。"
	}
	return "（Mock 模型）这一轮我带着 " + fmt.Sprint(countHistory(all)) + " 条历史消息在工作。多轮对话 = 每次都把「系统提示词 + 全部历史 + 新输入」发给模型。"
}

func countHistory(all string) int {
	n := 0
	for _, line := range strings.Split(all, "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}

// ------------------------------------------------------------- step06 路由

func mockRouter(last string) string {
	var intent string
	if strings.Contains(last, "天气") || strings.Contains(last, "weather") {
		intent = "weather"
	} else if exprRe.MatchString(last) {
		intent = "math"
	} else {
		intent = "chat"
	}
	return jw(map[string]any{
		"intent": intent,
		"reason": "根据用户输入关键词与语义判断下一步走向",
	})
}

// ------------------------------------------------------------- step09 MCP

func mockMcp(last string) string {
	// 工具结果回填轮：不再发起调用
	if strings.Contains(last, "工具结果") || strings.Contains(last, "Observation") {
		fact := extractAfter(last, "工具结果")
		fallback := clip(last)
		if strings.TrimSpace(fact) != "" {
			fallback = clip(fact)
		}
		return "基于 MCP Server 返回的事实：" + fallback + " —— 标准协议下的调用链闭环完成。"
	}
	if strings.Contains(last, "天气") || strings.Contains(last, "weather") {
		return jw(map[string]any{
			"method": "tools/call",
			"params": map[string]any{
				"name":      "get_weather",
				"arguments": map[string]any{"city": findCity(last)},
			},
		})
	}
	return "（Mock 模型）MCP 的核心是把「工具」标准化：统一注册、统一发现（tools/list）、统一调用（tools/call）。这个问题我直接回答即可，无需调用工具。"
}

// ------------------------------------------------------------- step10 技能

func mockSkill(all, last string) string {
	// 技能执行轮：travel_plan 技能内的模型调用按角色作答
	if strings.Contains(all, "旅行顾问") {
		return "杭州出行攻略：① 当前晴，27℃，适宜户外；② 建议上午游西湖，下午避晒逛馆；③ 早晚温差小，轻装出行即可。（由 L1 技能组合天气与时间工具产出）"
	}
	var skill string
	if strings.Contains(last, "旅行") || strings.Contains(last, "攻略") || strings.Contains(last, "旅游") {
		skill = "travel_plan"
	} else if strings.Contains(last, "天气") {
		skill = "weather_report"
	} else {
		skill = "free_chat"
	}
	return jw(map[string]any{
		"skill":  skill,
		"reason": "依据输入语义匹配已注册技能",
	})
}

// ------------------------------------------------------------- step07 RAG

func mockRag(all, last string) string {
	ctx := extractAfter(all, "【参考资料】")
	if strings.TrimSpace(ctx) != "" {
		firstLine := "相关资料"
		for _, line := range strings.Split(ctx, "\n") {
			firstLine = line
			break
		}
		return "根据检索到的资料：「" + clip(firstLine) + "」……\n\n" +
			"回答基于知识库片段 [1][2] 生成 —— 模型只负责「戴着资料说话」，事实由检索保证，这就是 RAG 解决模型幻觉与知识过期的思路。"
	}
	return "（Mock 模型）知识库中未命中相关片段时，我会如实回答不知道，而不是编造 —— 拒答也是 RAG 的重要能力。"
}

// ------------------------------------------------------------- step12 多智能体

func mockMulti(all, system string) string {
	if strings.Contains(system, "规划者") {
		return jw(map[string]any{
			"plan": []any{
				map[string]any{"role": "研究员", "task": "围绕主题收集要点"},
				map[string]any{"role": "写手", "task": "把要点组织成一篇短文"},
				map[string]any{"role": "审查员", "task": "检查事实与结构，不通过则打回"},
			},
		})
	}
	if strings.Contains(system, "研究员") {
		return "研究笔记：① 智能体的本质是模型驱动的循环；② 工具扩展了模型的行动边界；③ 工程化决定上限。"
	}
	if strings.Contains(system, "审查员") {
		return jw(map[string]any{"pass": true, "comment": "结构完整、表述准确，建议保留要点式风格"})
	}
	if strings.Contains(all, "研究笔记") {
		return "短文：智能体并不神秘 —— 它是一个让大模型「边想边做」的循环程序：模型负责决策，工具负责执行，记忆负责延续，工程负责兜底。（由写手智能体基于研究笔记产出）"
	}
	return "（Mock 模型）已收到任务。"
}

// ------------------------------------------------------------- step15 Loop + 守卫

func mockLoop(last, all string) string {
	if strings.Contains(all, "Observation:") {
		obs := extractAfter(all, "Observation:")
		return jw(map[string]any{
			"thought": "信息足够，输出最终答案",
			"final":   "循环结束，最终回答：" + clip(obs),
		})
	}
	return jw(map[string]any{
		"thought":      "进入运行时循环，先获取所需信息",
		"action":       "get_weather",
		"action_input": findCity(last),
	})
}

// ------------------------------------------------------------- step16 工作流

func mockWorkflow(all, last string) string {
	if strings.Contains(all, "[节点:classify]") {
		next := "faq"
		if strings.Contains(last, "天气") || exprRe.MatchString(last) {
			next = "tool"
		}
		return jw(map[string]any{"next": next, "reason": "意图分类节点给出路由"})
	}
	if strings.Contains(all, "[节点:tool]") {
		if m := exprFind.FindStringSubmatch(last); m != nil {
			return jw(map[string]any{
				"tool":      "calculator",
				"arguments": map[string]any{"expression": m[1]},
			})
		}
		return jw(map[string]any{
			"tool":      "get_weather",
			"arguments": map[string]any{"city": findCity(last)},
		})
	}
	if strings.Contains(all, "Observation:") {
		return "综合以上节点结果，这里是最终回答：" + clip(extractAfter(all, "Observation:")) + "（由 polish 节点整理输出）。"
	}
	if strings.Contains(all, "[节点:polish]") {
		base := ""
		for _, line := range strings.Split(extractAfter(all, "前序产出: "), "\n") {
			base = line
			break
		}
		if strings.TrimSpace(base) == "" {
			return "润色完成：「" + clip(last) + "」。"
		}
		return "润色完成：" + clip(base)
	}
	return "这是 FAQ 节点的直接回答。「" + clip(last) + "」属于常见问题，无需工具，命中预设答案后进入 polish 润色。"
}

// ------------------------------------------------------------- step11 工具注册表

// mockRegistry 与 step04 同一套 {"tool","arguments"} 协议，但工具清单来自注册表。
func mockRegistry(last string) string {
	if strings.Contains(last, "执行结果") {
		fact := extractAfter(last, "执行结果")
		fallback := clip(last)
		if strings.TrimSpace(fact) != "" {
			fallback = clip(fact)
		}
		return "根据工具返回的结果：" + fallback + " —— 执行器全程只查注册表，不认识任何具体工具。"
	}
	if strings.Contains(last, "天气") || strings.Contains(last, "weather") {
		return jw(map[string]any{
			"tool":      "get_weather",
			"arguments": map[string]any{"city": findCity(last)},
		})
	}
	if m := exprFind.FindStringSubmatch(last); m != nil {
		return jw(map[string]any{
			"tool":      "calculator",
			"arguments": map[string]any{"expression": m[1]},
		})
	}
	if strings.Contains(last, "几点") || strings.Contains(last, "时间") {
		return jw(map[string]any{
			"tool":      "get_time",
			"arguments": map[string]any{},
		})
	}
	return "这个问题不需要调用工具，直接回答即可 —— 工具清单由注册表动态生成，注册即生效、注销即消失。"
}

// ------------------------------------------------------------- step17 ReAct 运行时

// mockRuntime ReAct 协议；「死循环测试」会永远发起 echo 工具调用，触发 MAX_STEPS 保险丝。
func mockRuntime(all, last string) string {
	// echo 的观察结果继续发起 echo —— 模拟失控场景，直到保险丝起跳
	if strings.HasPrefix(last, "Observation: echo:") {
		return jw(map[string]any{
			"thought":      "任务还没完成，继续调用 echo（模拟失控）",
			"action":       "echo",
			"action_input": "tick",
		})
	}
	if strings.Contains(last, "死循环") || strings.Contains(last, "循环测试") {
		return jw(map[string]any{
			"thought":      "这个任务永远做不完（模拟失控场景）",
			"action":       "echo",
			"action_input": "tick",
		})
	}
	if strings.Contains(all, "Observation:") {
		obs := extractAfter(all, "Observation:")
		return jw(map[string]any{
			"thought": "已拿到观察结果，信息足够，结束本回合",
			"final":   "根据工具查询结果：" + clip(obs) + "（turn 以 TurnEndReason.Completed 收场）",
		})
	}
	if m := exprFind.FindStringSubmatch(last); m != nil {
		return jw(map[string]any{
			"thought":      "有算式，调用计算器，不能心算",
			"action":       "calculator",
			"action_input": m[1],
		})
	}
	if strings.Contains(last, "天气") || strings.Contains(last, "weather") {
		return jw(map[string]any{
			"thought":      "需要实时数据，先调天气工具",
			"action":       "get_weather",
			"action_input": findCity(last),
		})
	}
	return jw(map[string]any{
		"thought": "普通对话无需工具，直接作答",
		"final":   "（Mock 模型）本轮 " + fmt.Sprint(countHistory(all)) + " 条历史的上下文已按预算裁剪后发给模型。多聊几轮，可在轨迹里看到「上下文裁剪」事件。",
	})
}

// ------------------------------------------------------------- step19 人工介入

// mockAsk 缺关键信息时发起 ask_user_question；「人工答复：」回填后带着完整信息完成任务。
func mockAsk(all, last string) string {
	if strings.Contains(all, "人工答复：") {
		ans := "上海"
		for i, line := range strings.Split(extractAfter(all, "人工答复："), "\n") {
			if i == 0 {
				ans = line
			}
			break
		}
		from := clip(ans)
		switch {
		case strings.Contains(ans, "上海"):
			from = "上海"
		case strings.Contains(ans, "杭州"):
			from = "杭州"
		case strings.Contains(ans, "广州"):
			from = "广州"
		}
		return jw(map[string]any{
			"final": "已为你预订：" + from + " → 北京，明日 08:30 起飞，经济舱 ¥1,280（模拟出票）。" +
				"刚才缺出发地时我没有瞎猜，而是问了人 —— 这就是 ask_user_question 的人机协同闭环。",
		})
	}
	if strings.Contains(last, "机票") || strings.Contains(last, "订") || strings.Contains(last, "出行") {
		return jw(map[string]any{
			"ask": "预订机票需要确认出发地 —— 请问从哪个城市出发？（可选：北京 / 上海 / 杭州 / 广州）",
		})
	}
	return jw(map[string]any{
		"final": "（Mock 模型）信息完整的问题我直接回答。想看人工介入：发「帮我订一张去北京的机票」，我会停下来向你确认出发地。",
	})
}

// ------------------------------------------------------------- step20 审批门禁

func mockApproval(last string) string {
	if strings.Contains(last, "执行结果") || strings.Contains(last, "审批结果") {
		return jw(map[string]any{
			"final": "任务完成：" + clip(last) + " —— 注意轨迹里的权限矩阵：安全工具自动放行，" +
				"高危工具必须过审批门禁，这是生产级智能体与玩具的分水岭。",
		})
	}
	if strings.Contains(last, "邮件") || strings.Contains(last, "发送") || strings.Contains(last, "发一封") {
		return jw(map[string]any{
			"tool":      "send_email",
			"arguments": "to=team@xiaofuge.cn, content=本周周报",
		})
	}
	if strings.Contains(last, "删除") || strings.Contains(last, "清理") {
		return jw(map[string]any{
			"tool":      "delete_file",
			"arguments": "path=workspace/tmp/old.log",
		})
	}
	if strings.Contains(last, "天气") {
		return jw(map[string]any{
			"tool":      "get_weather",
			"arguments": "city=" + findCity(last),
		})
	}
	return jw(map[string]any{
		"final": "（Mock 模型）想看审批门禁：发「给团队发一封周报邮件」（高危，会请求审批），" +
			"回复「批准 / 拒绝 / 批准并记住」观察三种走向。",
	})
}

// ------------------------------------------------------------- step21 沙箱

func mockSandbox(last string) string {
	if strings.Contains(last, "执行结果") || strings.Contains(last, "已拦截") || strings.Contains(last, "沙箱内") {
		return jw(map[string]any{"final": clip(last)})
	}
	return jw(map[string]any{
		"final": "（Mock 模型）沙箱采用纵深防御：策略 → 黑名单 → 边界 → 规范化四层拦截。" +
			"发「执行：ls sandbox」看放行，或「执行：rm -rf /」看第 2 层拦截。",
	})
}

// ------------------------------------------------------------- step22 事件溯源

func mockEvent(all, last string) string {
	remembered := ""
	for _, m := range nameFind.FindAllStringSubmatch(all, -1) {
		n := m[1]
		if strings.HasPrefix(n, "什么") || strings.HasPrefix(n, "名字") || strings.HasPrefix(n, "谁") {
			continue
		}
		remembered = n
	}
	if strings.Contains(last, "我叫") {
		return jw(map[string]any{
			"final": "你好，" + remembered + "！这次对话连同你的名字已作为事件写入 JSONL 日志 ——" +
				"我的记忆不是内存变量，而是从日志投影出来的视图，服务重启也不丢。",
		})
	}
	if strings.Contains(last, "叫什么") || strings.Contains(last, "记得") {
		if remembered != "" {
			return jw(map[string]any{"final": "你叫「" + remembered + "」。这条记忆是我刚才从事件日志回放重建的 —— 状态即日志。"})
		}
		return jw(map[string]any{"final": "日志里还没有你的名字，先说「我叫XX」吧。"})
	}
	return jw(map[string]any{
		"final": "（Mock 模型）本轮会话由事件日志回放重建。多聊几轮再看 events/step22-events.jsonl，" +
			"每一条对话都是不可变事件；「重置」删掉日志，记忆归零。",
	})
}

// ------------------------------------------------------------- step23 插件机制

// mockPlugin 插件机制剧本：安装后正常决策调用插件工具；未安装/已卸载时由注册表未命中路径兜底。
func mockPlugin(all, last string) string {
	if strings.Contains(last, "执行结果") {
		fact := extractAfter(last, "执行结果")
		fallback := clip(last)
		if strings.TrimSpace(fact) != "" {
			fallback = clip(fact)
		}
		return "根据插件工具返回的结果：" + fallback +
			" —— 这个工具不在宿主代码里，而是插件经独立 ClassLoader 装进注册表的。"
	}
	if strings.Contains(last, "掷") || strings.Contains(last, "硬币") || strings.Contains(last, "coin") {
		return jw(map[string]any{"tool": "coin_flip", "arguments": map[string]any{}})
	}
	if strings.Contains(last, "运行") || strings.Contains(last, "多久") || strings.Contains(last, "uptime") {
		return jw(map[string]any{"tool": "uptime", "arguments": map[string]any{}})
	}
	return "（Mock 模型）先「安装插件」，工具才会出现在我的清单里；「卸载插件」后我再也看不见它们 —— 能力即插即拔。"
}

// ------------------------------------------------------------- step25 CLI 智能体

// mockCli CLI 剧本：命令类输入返回 run_command 决策；其余兜底引导。
func mockCli(all, last, system string) string {
	if strings.Contains(last, "执行结果") {
		fact := extractAfter(last, "执行结果")
		fallback := clip(last)
		if strings.TrimSpace(fact) != "" {
			fallback = clip(fact)
		}
		return "命令结果：" + fallback +
			" —— run_command 在受限工作区执行，Step21 的四层防御全程护航。"
	}
	if strings.Contains(last, "列出") || strings.Contains(last, "文件") || strings.Contains(last, "目录") || strings.Contains(last, "ls") {
		return jw(map[string]any{
			"tool":      "run_command",
			"arguments": map[string]any{"command": "ls"},
		})
	}
	if strings.Contains(last, "几点") || strings.Contains(last, "时间") || strings.Contains(last, "日期") {
		return jw(map[string]any{
			"tool":      "run_command",
			"arguments": map[string]any{"command": "date"},
		})
	}
	if strings.Contains(last, "hello") {
		return jw(map[string]any{
			"tool":      "run_command",
			"arguments": map[string]any{"command": "cat hello.txt"},
		})
	}
	return "我是终端里的 CLI 智能体。试试「运行：ls」，或让我「列出工作区文件」—— 命令都在 sandbox/step25 里执行，危险命令会被四层防御拦截。"
}

// ------------------------------------------------------------- step13 子代理

// mockSubagent 子代理剧本：system 带【子代理身份】为子代理回合（fork 能看到父会话里的紫色）；否则为主代理回合。
func mockSubagent(all, last, system string) string {
	if strings.Contains(system, "子代理身份") {
		forkKnows := strings.Contains(system, "继承的父会话上下文") && strings.Contains(system, "紫色")
		if strings.Contains(system, "写作子代理") {
			if forkKnows {
				return "愿你的每一天都被喜爱的事物环绕 —— 像那一抹恰到好处的紫色。（fork：我从父会话记得你最喜欢紫色）"
			}
			return "愿你所行皆坦途，所遇皆温暖，往后的每一天都值得期待。（spawn：我只知道任务本身）"
		}
		return "Agent 调研要点：1) 工具标准化（MCP）成为默认范式；2) 运行时基座（审批/沙箱/事件）走向生产化；3) 多代理协作从固定流水线走向动态派遣。"
	}
	if strings.Contains(last, "执行结果") {
		fact := extractAfter(last, "执行结果")
		body := fact
		if strings.TrimSpace(fact) == "" {
			body = clip(last)
		}
		return "子代理已完成任务，结果如下：\n" + body
	}
	if strings.Contains(last, "fork") || strings.Contains(last, "继承") {
		return jw(map[string]any{"tool": "dispatch_subagent", "name": "writer", "mode": "fork", "task": "写一句贺词"})
	}
	if strings.Contains(last, "spawn") || strings.Contains(last, "全新") {
		return jw(map[string]any{"tool": "dispatch_subagent", "name": "writer", "mode": "spawn", "task": "写一句贺词"})
	}
	if strings.Contains(last, "调研") || strings.Contains(last, "研究") || strings.Contains(last, "趋势") {
		return jw(map[string]any{"tool": "dispatch_subagent", "name": "researcher", "mode": "spawn", "task": "调研 Agent 趋势"})
	}
	if strings.Contains(last, "记住") || strings.Contains(last, "最喜欢") {
		return "好的，已记下。这条信息已进入父会话上下文 —— 之后 fork 出去的子代理都能看到它。"
	}
	if strings.Contains(last, "对比") {
		return "两次贺词的差异就是 spawn 与 fork 的差异：fork 的子代理继承了「最喜欢紫色」，贺词带了紫色；spawn 的子代理是全新实例，只会写通用贺词。"
	}
	return "（Mock 模型）试试：「fork 一个子代理，让它写一句贺词」或「spawn 一个子代理写贺词」，体会两种派遣的上下文差异。"
}

// ------------------------------------------------------------- step24 钩子体系

// mockHooks 钩子讲解员：工具调用走宿主直连路由（查询：/发送：/钩子：），模型只负责讲解引导。
func mockHooks(all, last, system string) string {
	if strings.Contains(last, "执行结果") {
		fact := extractAfter(last, "执行结果")
		fallback := clip(last)
		if strings.TrimSpace(fact) != "" {
			fallback = clip(fact)
		}
		return "钩子管线说明：" + fallback
	}
	return jw(map[string]any{"final": "本场景的钩子挂在工具调用的 before / after 生命周期上。试试：\n" +
		"·「查询：张三」—— after 脱敏钩子把手机号/邮箱打码\n" +
		"·「发送：给 13812345678 发 密码是123456」—— before 风控钩子直接拦截\n" +
		"·「钩子：关」再「查询：张三」—— 摘掉钩子看明文，对比出钩子的价值\n" +
		"·「审计日志」—— before 审计钩子记下的每一次调用"})
}

// ------------------------------------------------------------- step14 A2A 协作

// mockA2a A2A 剧本：先发现名片，再按任务类型路由到 translator / weather。
func mockA2a(all, last, system string) string {
	if strings.Contains(last, "执行结果") {
		fact := extractAfter(last, "执行结果")
		body := fact
		if strings.TrimSpace(fact) == "" {
			body = clip(last)
		}
		return "外部代理已通过 A2A 回执：" + body + " —— 对方有自己的模型与工具，我们只认 task_id 与回执。"
	}
	if strings.Contains(last, "发现") || strings.Contains(last, "名片") || strings.Contains(last, "附近") {
		return jw(map[string]any{"tool": "a2a_discover"})
	}
	if strings.Contains(last, "翻译") {
		return jw(map[string]any{
			"tool":  "a2a_send",
			"agent": "translator",
			"task":  "把「你好，智能体」翻译成英文",
		})
	}
	if strings.Contains(last, "天气") || strings.Contains(last, "户外") {
		return jw(map[string]any{
			"tool":  "a2a_send",
			"agent": "weather",
			"task":  "明天适合户外运动吗？",
		})
	}
	return jw(map[string]any{
		"final": "我是主代理，可以经 A2A 协议与外部代理协作。试试：「发现一下附近的代理」，" +
			"或直接「让翻译代理把……翻译成英文」「问问天气代理明天适合户外运动吗」。",
	})
}

// ------------------------------------------------------------- step26 定时工具

// mockSchedule 定时讲解员：调度走宿主直连路由（定时：/循环：/任务列表/取消：），模型只负责讲解引导。
func mockSchedule(all, last, system string) string {
	if strings.Contains(last, "执行结果") {
		fact := extractAfter(last, "执行结果")
		fallback := clip(last)
		if strings.TrimSpace(fact) != "" {
			fallback = clip(fact)
		}
		return "定时任务说明：" + fallback
	}
	return jw(map[string]any{"final": "本场景给工具装上了时间维度。试试：\n" +
		"·「定时：5秒 提醒我喝水」—— 一次性调度，拿 job_id\n" +
		"·「任务列表」—— 看 pending / 已触发次数\n" +
		"·「循环：每10秒 报一次时」—— 周期任务\n" +
		"·「取消：job-1」「完成了什么」—— 管理与回看（触发结果落盘 events/step26-tasks.jsonl）"})
}

// ------------------------------------------------------------- 工具方法

// jw JSON 序列化（等价 Java Json.write，Go 内置 encoding/json 实现）。
func jw(v any) string {
	return Write(v)
}

// cityWeather 城市天气表（Java 为 Map.of，顺序不保证；Go 用固定切片，检索顺序与书写顺序一致）。
var cityWeather = []struct{ city, weather string }{
	{"北京", "晴，26℃"}, {"上海", "多云，28℃"}, {"广州", "雷阵雨，31℃"},
	{"深圳", "阵雨，30℃"}, {"杭州", "晴，27℃"}, {"成都", "阴，22℃"},
}

func findCity(text string) string {
	for _, c := range cityWeather {
		if strings.Contains(text, c.city) {
			return c.city
		}
	}
	return "北京"
}

func extractAfter(text, marker string) string {
	idx := strings.LastIndex(text, marker)
	if idx < 0 {
		return ""
	}
	v := text[idx+len(marker):]
	v = leadingSpaceColonRe.ReplaceAllString(v, "")
	return strings.TrimSpace(v)
}

var leadingSpaceColonRe = regexp.MustCompile(`^[\s:：]+`)

func clip(s string) string {
	one := strings.TrimSpace(wsRe.ReplaceAllString(s, " "))
	r := []rune(one)
	if len(r) > 60 {
		return string(r[:60]) + "…"
	}
	return one
}

var (
	// exprRe 判断输入中是否含算式（等价 Java last.matches(".*\\d+\\s*[+\\-*/]\\s*\\d+.*")）
	exprRe = regexp.MustCompile(`\d+\s*[+\-*/]\s*\d+`)
	// exprFind 提取第一个算式（等价 Java Pattern.compile("(\\d+\\s*[+\\-*/]\\s*\\d+)").find()）
	exprFind = regexp.MustCompile(`(\d+\s*[+\-*/]\s*\d+)`)
	// nameFind 提取"我叫X"（等价 Java 我叫([\u4e00-\u9fa5A-Za-z0-9]{1,10})）
	nameFind = regexp.MustCompile(`我叫([\x{4e00}-\x{9fa5}A-Za-z0-9]{1,10})`)
	// 「我叫什么/名字/谁/啥」是询问而非自报姓名，路由到回忆分支
	nameQuestionRe = regexp.MustCompile(`我叫(?:什么|名字|谁|啥)`)
	// wsRe 等价 Java replaceAll("\\s+", " ")
	wsRe = regexp.MustCompile(`\s+`)
)
