// Step08 · LLM-Wiki 智能体 —— 知识编译与持久化。
//
// RAG 解决「检索」：每次提问都从头翻文档。LLM-Wiki 解决「沉淀」：
// 项目知识（架构约定、代码规范、部署流程）被编译成结构化条目，
// 持久保存、随启动加载、增量维护 —— 知识编译一次、持续复用，而非每次从头检索。
// 思路源自 Andrej Karpathy 的 LLM-Wiki 提法（AGENTS.md / CLAUDE.md 即其工程形态）。
//
// 本场景：内置基础 Wiki（编译一次）；用户说「编译：主题：内容」即可新增条目，
// 持久化到 wiki/user-wiki.md，重启不丢；提问时按相关度取条目注入提示词作答。
// 教学版检索用字符 bigram 重合度模拟，换成 Embedding 即生产级实现。
package step08

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/fuzhengwei/ToyAgent-Go/agent"
	"github.com/fuzhengwei/ToyAgent-Go/llm"
)

// Impl Wiki 实现：userWiki 是实例字段（重启后从 wiki/user-wiki.md 恢复）。
type Impl struct {
	model     llm.ChatModel
	lastTrace []map[string]any

	// wikiEntry 用户编译的增量条目（保持插入顺序，等价 Java LinkedHashMap）。
	userWiki   map[string]string
	userOrder  []string
}

// wikiEntry currentWiki 的元素。
type wikiEntry struct {
	topic, content string
}

// baseEntry 基础 Wiki 条目结构。
type baseEntry = wikiEntry

// baseWiki 基础 Wiki：项目知识，启动时编译一次
// （生产中来自仓库里的 AGENTS.md / CLAUDE.md；Java 为 Map.of 顺序随机，Go 固定顺序）。
var baseWiki = []baseEntry{
	{"架构约定", "ToyAgent 是零依赖教学项目：一个接口类就是一个智能体场景（step01-26），全部实现放在 Impl 内部类里，禁止引入第三方依赖；模型层统一走 ChatModel 接口。"},
	{"代码规范", "Java 17 语法；类与核心方法必须有中文 Javadoc 说明教学意图；trace 事件用 type/label/detail 三段式；JSON 一律走自研 Json 工具类。"},
	{"部署流程", "javac -d target/classes 编译后 java -cp target/classes cn.xiaofuge.ai.Application 启动，默认端口 8099，可用环境变量 TOY_AGENT_PORT 覆盖；前端静态资源在 web/ 目录由服务直接托管。"},
	{"业务规则", "配置只保存在各人浏览器 localStorage 随请求生效，服务端 config.properties 仅作默认兜底；未配置 API Key 时所有场景回落 Mock 演示模型。"},
}

const userWikiFile = "wiki/user-wiki.md"

const systemPrompt = `你是项目 Wiki 问答智能体。回答规则：
1. 优先依据【项目 Wiki】的条目回答，并标注来源条目名，如（来源：架构约定）。
2. Wiki 条目不足以回答时，如实说"项目 Wiki 中暂无相关条目"，并提示可以用「编译：主题：内容」沉淀该知识。禁止编造。
`

// New 构造 Step08 智能体（构造时加载已编译条目）。
func New(model llm.ChatModel) agent.Agent {
	a := &Impl{model: model, userWiki: map[string]string{}}
	a.loadUserWiki()
	return a
}

func (a *Impl) Name() string {
	return "Step08 · LLM-Wiki 知识编译"
}

func (a *Impl) Chat(input string) string {
	a.lastTrace = nil

	// ---------------- 编译指令：把对话中的知识沉淀进 Wiki ----------------
	if strings.HasPrefix(input, "编译：") || strings.HasPrefix(input, "编译:") ||
		strings.HasPrefix(input, "记住：") || strings.HasPrefix(input, "记住:") {
		return a.compile(input)
	}

	// ---------------- 加载 Wiki（启动编译一次 + 增量条目） ----------------
	topics := a.currentWiki()
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "load", "label": "加载项目 Wiki",
		"detail": fmt.Sprintf("共 %d 个条目（内置 %d + 已编译 %d），无需每次重新检索原始文档",
			len(topics), len(baseWiki), len(a.userOrder))})

	// ---------------- 相关条目选取（教学版 bigram 打分，生产换 Embedding） ----------------
	type hit struct {
		topic, content string
		score          float64
	}
	ranked := make([]hit, 0, len(topics))
	for _, e := range topics {
		ranked = append(ranked, hit{e.topic, e.content, score(e.topic+e.content, input)})
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
		parts = append(parts, fmt.Sprintf("%s（%.0f 分）", h.topic, h.score*100))
	}
	retrieveDetail := "无相关条目"
	if len(parts) > 0 {
		retrieveDetail = strings.Join(parts, " ｜ ")
	}
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "retrieve", "label": "命中条目", "detail": retrieveDetail})

	// ---------------- 增强生成 ----------------
	messages := []llm.Message{llm.System(systemPrompt)}
	if len(hits) > 0 {
		ctx := "【项目 Wiki】\n"
		for _, h := range hits {
			ctx += "◆ " + h.topic + "：" + h.content + "\n"
		}
		messages = append(messages, llm.User(ctx))
	}
	messages = append(messages, llm.User(input))

	answer, err := llm.SafeChat(a.model, messages)
	if err != nil {
		answer = err.Error()
	}
	finalDetail := "模型按约定提示可编译新条目"
	if len(hits) > 0 {
		finalDetail = fmt.Sprintf("回答基于 %d 个 Wiki 条目", len(hits))
	}
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "final", "label": "基于 Wiki 作答完成", "detail": finalDetail})
	return answer
}

// compile 编译：解析「编译：主题：内容」，写入 Wiki 并持久化。
func (a *Impl) compile(input string) string {
	// 去掉前缀「编译：/记住：」（等宽 3 字符，按字符切分）
	body := strings.TrimSpace(string([]rune(input)[3:]))
	runes := []rune(body)
	sep := strings.IndexRune(body, '：')
	if sep < 0 {
		sep = strings.IndexByte(body, ':')
	}
	// sep 换算为 rune 下标（中文冒号占 3 字节）
	sepRune := sep
	if sep >= 0 {
		sepRune = len([]rune(body[:sep]))
	}
	if body == "" || sepRune <= 0 || sepRune == len(runes)-1 {
		a.lastTrace = append(a.lastTrace, map[string]any{
			"type": "compile", "label": "编译失败", "detail": "格式：编译：主题：内容"})
		return "编译格式不对哦。请用：「编译：主题：内容」，例如「编译：发布流程：每周三灰度，周五全量上线」。"
	}
	bodyRunes := []rune(body)
	topic := strings.TrimSpace(string(bodyRunes[:sepRune]))
	content := strings.TrimSpace(string(bodyRunes[sepRune+1:]))

	_, update := a.userWiki[topic]
	if !update {
		a.userOrder = append(a.userOrder, topic)
	}
	a.userWiki[topic] = content
	a.persistUserWiki()

	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "compile", "label": map[bool]string{true: "更新条目", false: "新增条目"}[update],
		"detail": "◆ " + topic + "（已持久化到 " + userWikiFile + "，重启不丢）"})
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "final", "label": "知识编译完成",
		"detail": fmt.Sprintf("Wiki 现有 %d 个条目，后续提问直接复用", len(baseWiki)+len(a.userOrder))})
	return "已编译进项目 Wiki：◆ " + topic + "\n\n" + content + "\n\n之后所有提问都会带上这条知识（已持久化，重启不丢）。试试问我相关内容。"
}

func (a *Impl) currentWiki() []wikiEntry {
	all := make([]wikiEntry, 0, len(baseWiki)+len(a.userOrder))
	all = append(all, baseWiki...)
	for _, k := range a.userOrder {
		all = append(all, wikiEntry{k, a.userWiki[k]})
	}
	return all
}

// persistUserWiki 持久化用户编译条目（教学版写本地 markdown；生产中提交到代码仓库）。
func (a *Impl) persistUserWiki() {
	if err := os.MkdirAll(filepath.Dir(userWikiFile), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "[ToyAgent] 写入 user-wiki 失败: "+err.Error())
		return
	}
	var sb strings.Builder
	sb.WriteString("# ToyAgent 用户 Wiki（由页面编译生成）\n\n")
	for _, k := range a.userOrder {
		sb.WriteString("## " + k + "\n" + a.userWiki[k] + "\n\n")
	}
	if err := os.WriteFile(userWikiFile, []byte(sb.String()), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "[ToyAgent] 写入 user-wiki 失败: "+err.Error())
	}
}

func (a *Impl) loadUserWiki() {
	data, err := os.ReadFile(userWikiFile)
	if err != nil {
		return
	}
	topic := ""
	var content strings.Builder
	flush := func() {
		if topic != "" {
			if _, exists := a.userWiki[topic]; !exists {
				a.userOrder = append(a.userOrder, topic)
			}
			a.userWiki[topic] = strings.TrimSpace(content.String())
		}
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "## ") {
			flush()
			topic = strings.TrimSpace(strings.TrimPrefix(line, "## "))
			content.Reset()
		} else if topic != "" && !strings.HasPrefix(line, "#") {
			content.WriteString(line)
			content.WriteString("\n")
		}
	}
	flush()
	if len(a.userOrder) > 0 {
		fmt.Printf("[ToyAgent] 已从 %s 恢复 %d 个编译条目\n", userWikiFile, len(a.userOrder))
	}
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

func (a *Impl) Trace() []map[string]any {
	return agent.CopyTrace(a.lastTrace)
}

func (a *Impl) Reset() {
	a.userWiki = map[string]string{}
	a.userOrder = nil
	_ = os.Remove(userWikiFile)
	a.lastTrace = nil
}
