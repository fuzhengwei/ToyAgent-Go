// Step07 · RAG 智能体 —— 检索增强生成。
//
// 模型的知识是静态的，私有知识它根本没见过。RAG 的思路：
// 先把问题在知识库里检索出最相关的片段，再把片段作为"参考资料"
// 塞进提示词，让模型"戴着资料说话"。回答有出处、知识可热更。
//
// 完整 RAG = 切分 → 向量化 → 相似检索 → 增强生成。本场景用
// 关键词重合度模拟向量相似度（教学简化），骨架完全一致 ——
// 换成 Embedding 检索即生产级实现。
package step07

import (
	"fmt"
	"sort"
	"strings"

	"github.com/fuzhengwei/ToyAgent-Go/agent"
	"github.com/fuzhengwei/ToyAgent-Go/llm"
)

// Impl RAG 实现。
type Impl struct {
	model     llm.ChatModel
	lastTrace []map[string]any
}

// knowledge 私有知识库：真实系统存向量库，这里用内存文档集合演示。
var knowledge = []string{
	"ToyAgent 是小傅哥的智能体教学项目，用 12 个渐进式场景讲清楚智能体实现，每个场景只有一个接口类。",
	"智能体 = 大模型（大脑）+ 工具（手脚）+ 记忆（延续）+ 循环（节拍）。ReAct 循环是智能体的经典执行模式。",
	"MCP（Model Context Protocol）是 Anthropic 提出的工具标准化协议，核心方法为 tools/list 与 tools/call，实现工具与智能体解耦。",
	"RAG（检索增强生成）先从知识库检索相关片段，再把片段注入提示词让模型基于事实回答，能缓解幻觉与知识过期问题。",
	"Function Calling 让模型输出结构化工具调用请求（工具名 + 参数），由外部程序执行后将结果回填给模型。",
	"上下文窗口是模型一次能处理的最大 Token 数，超出时需要用滑动窗口、摘要压缩等上下文工程手段保护关键信息。",
}

const systemPrompt = `你是知识库问答智能体。回答规则：
1. 优先依据【参考资料】回答，并标注来源编号如 [1]。
2. 资料不足以回答时，如实说"知识库中暂无相关内容"，禁止编造。
`

// New 构造 Step07 智能体。
func New(model llm.ChatModel) agent.Agent {
	return &Impl{model: model}
}

func (a *Impl) Name() string {
	return "Step07 · RAG 检索增强生成"
}

func (a *Impl) Chat(input string) string {
	a.lastTrace = nil

	// 1. 检索：按关键词重合度给知识片段打分，取 Top2
	type hit struct {
		text  string
		score float64
	}
	ranked := make([]hit, 0, len(knowledge))
	for _, text := range knowledge {
		ranked = append(ranked, hit{text, score(text, input)})
	}
	sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].score > ranked[j].score })
	if len(ranked) > 2 {
		ranked = ranked[:2]
	}
	var hits []hit
	for _, h := range ranked {
		if h.score > 0 {
			hits = append(hits, h)
		}
	}

	var parts []string
	for _, h := range hits {
		parts = append(parts, fmt.Sprintf("%.0f 分 · %s", h.score*100, clip(h.text, 30)))
	}
	retrieveDetail := "未命中任何片段（得分均为 0）"
	if len(parts) > 0 {
		retrieveDetail = strings.Join(parts, " ｜ ")
	}
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "retrieve", "label": "知识库检索", "detail": retrieveDetail})

	// 2. 增强：命中片段注入提示词；未命中则要求拒答
	messages := []llm.Message{llm.System(systemPrompt)}
	if len(hits) > 0 {
		ctx := "【参考资料】\n"
		for i, h := range hits {
			ctx += fmt.Sprintf("[%d] %s\n", i+1, h.text)
		}
		messages = append(messages, llm.User(ctx))
	}
	messages = append(messages, llm.User(input))

	// 3. 生成
	answer, err := llm.SafeChat(a.model, messages)
	if err != nil {
		answer = err.Error()
	}
	finalDetail := "模型按约定拒答"
	if len(hits) > 0 {
		finalDetail = fmt.Sprintf("回答基于 %d 个知识片段", len(hits))
	}
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "final", "label": "增强生成完成", "detail": finalDetail})
	return answer
}

// score 教学版相似度：字符 bigram 重合率。生产中替换为 Embedding 余弦相似度。
func score(text, query string) float64 {
	runes := []rune(query)
	if len(runes) < 2 {
		return 0
	}
	grams := map[string]struct{}{}
	for i := 0; i < len(runes)-1; i++ {
		grams[string(runes[i:i+2])] = struct{}{}
	}
	hitCount := 0
	for g := range grams {
		if strings.Contains(text, g) {
			hitCount++
		}
	}
	if len(grams) == 0 {
		return 0
	}
	return float64(hitCount) / float64(len(grams))
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

func (a *Impl) Trace() []map[string]any {
	return agent.CopyTrace(a.lastTrace)
}

func (a *Impl) Reset() {}
