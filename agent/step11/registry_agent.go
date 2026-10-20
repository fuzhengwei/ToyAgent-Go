// Step11 · 工具注册表 —— 用「协议 + 注册表」取代写死的 switch。
//
// 与 Step04 的三点不同：
//  1. 工具实现 ToolDefinition 协议，注册进 ToolRegistry；
//  2. 系统提示词里的工具清单由注册表自动生成 —— 注册即生效、注销即消失；
//  3. 执行器只做 lookup + execute，不再认识任何具体工具。
//
// 本包同时导出 ToolDefinition 协议与 ToolRegistry，被 step17 / step23 复用。
//
// 试试本场景的本地指令：
//
//	查看工具清单     —— 看注册表当前内容（也是模型每次看到的清单）
//	卸载：get_time   —— 用注销器摘除工具，再问时间模型就说"没有该工具了"
package step11

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fuzhengwei/ToyAgent-Go/agent"
	"github.com/fuzhengwei/ToyAgent-Go/llm"
)

// ToolDefinition 工具定义协议 —— 一个接口 = 一个工具。
//
// Step03/04 里工具是写死在 switch 分支里的：加一个工具要改执行器代码。
// 本场景把"工具"抽象成统一协议：
//   - Name() 工具名 —— 模型调用时使用的标识；
//   - Description() 用途描述 —— 模型据此判断"该不该用这个工具"；
//   - Parameters() 参数说明 —— 生产中是 JSON Schema，教学化简为「参数名 → 类型说明」；
//   - Execute() 执行逻辑 —— 工具的真正手脚。
//
// 实现这个接口 + 注册进注册表 = 一个模型可见、可调用的工具。
// MCP 协议的 tools/list + tools/call，本质就是这套协议的标准化版本（见场景 09）。
type ToolDefinition interface {
	// Name 工具名：模型输出 {"tool": "名字"} 时的标识。
	Name() string
	// Description 一句话用途：会进入系统提示词，是模型选择工具的唯一依据。
	Description() string
	// Parameters 参数说明：教学化简版 JSON Schema（参数名 → 类型与含义）。
	Parameters() map[string]string
	// Execute 执行工具：输入参数，输出给模型看的观察结果。失败返回 error（对应 Java 抛异常）。
	Execute(args map[string]any) (string, error)
	// ConcurrencySafe 是否可并行执行（fs 读类 true，shell 写类 false）—— 预留并行调度语义。
	// Java 版为接口 default 方法，Go 需显式实现（默认返回 false）。
	ConcurrencySafe() bool
}

// ToolRegistry 工具注册表 —— 模型"看得见"的工具目录。
//
// 三个职责：
//  1. 注册：Register 把工具放进目录，并返回一个「注销器 func()」—— 持有它就能把
//     工具从目录摘除。插件停止/卸载时，宿主靠这个句柄回收注册项（注册-回收对称性）；
//  2. 发现：Lookup 按名字取工具，执行器只认注册表、不认具体工具；
//  3. 清单生成：PromptCatalog 把目录渲染成系统提示词里的工具清单 ——
//     模型能看见哪些工具，完全由注册表当前内容决定。注册即生效，注销即消失。
type ToolRegistry struct {
	mu    sync.Mutex
	order []string // 保持注册顺序（等价 Java LinkedHashMap）
	tools map[string]ToolDefinition
}

// NewToolRegistry 构造注册表。
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{tools: map[string]ToolDefinition{}}
}

// Register 注册工具，返回注销器（disposer）：调用它即把该工具从目录摘除。
func (r *ToolRegistry) Register(tool ToolDefinition) func() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.tools == nil {
		r.tools = map[string]ToolDefinition{}
	}
	if _, exists := r.tools[tool.Name()]; !exists {
		r.order = append(r.order, tool.Name())
	}
	r.tools[tool.Name()] = tool
	return func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		delete(r.tools, tool.Name())
	}
}

// Lookup 按名字查找工具；未命中返回 nil。
func (r *ToolRegistry) Lookup(name string) ToolDefinition {
	r.mu.Lock()
	defer r.mu.Unlock()
	if name == "" {
		return nil
	}
	return r.tools[name]
}

// Size 注册表内工具数量。
func (r *ToolRegistry) Size() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.tools)
}

// IsEmpty 是否为空。
func (r *ToolRegistry) IsEmpty() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.tools) == 0
}

// Clear 清空注册表（重置场景时使用）。
func (r *ToolRegistry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools = map[string]ToolDefinition{}
	r.order = nil
}

// PromptCatalog 把当前目录渲染成系统提示词中的工具清单 ——
// 工具增删后无需改任何提示词代码，下一次对话模型自动"看见"变化。
func (r *ToolRegistry) PromptCatalog() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var sb strings.Builder
	for _, name := range r.order {
		t, ok := r.tools[name]
		if !ok {
			continue
		}
		sb.WriteString("- " + t.Name() + "(")
		i := 0
		for k, v := range t.Parameters() {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(k + ": " + v)
			i++
		}
		sb.WriteString(") — " + t.Description() + "\n")
	}
	return sb.String()
}

// ------------------------------------------------------------------ RegistryAgent 场景

// Impl 工具注册表场景实现。
type Impl struct {
	model     llm.ChatModel
	registry  *ToolRegistry
	disposers map[string]func()
	lastTrace []map[string]any
}

// systemPromptPrefix 系统提示词前缀。
const systemPromptPrefix = `你是一个可以调用工具的智能体。可用工具清单（由注册表自动生成）：
`

// New 构造 Step11 智能体并注册内置工具。
func New(model llm.ChatModel) agent.Agent {
	a := &Impl{model: model, registry: NewToolRegistry(), disposers: map[string]func(){}}
	a.registerDefaults()
	return a
}

// registerDefaults 注册内置工具：注册这个动作本身，就是本场景的主角。
func (a *Impl) registerDefaults() {
	a.disposers["get_weather"] = a.registry.Register(&WeatherTool{})
	a.disposers["calculator"] = a.registry.Register(&CalculatorTool{})
	a.disposers["get_time"] = a.registry.Register(&TimeTool{})
}

func (a *Impl) Name() string {
	return "Step11 · 工具注册表（ToolDefinition 协议）"
}

func (a *Impl) Chat(input string) string {
	a.lastTrace = nil
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "tool", "label": "注册表就绪",
		"detail": fmt.Sprintf("当前 %d 个工具，清单由注册表自动生成，无需改提示词代码", a.registry.Size())})

	// ---- 本地指令 1：查看工具清单（不经过模型，直接看注册表） ----
	if strings.Contains(input, "查看工具") || strings.Contains(input, "工具清单") {
		a.lastTrace = append(a.lastTrace, map[string]any{
			"type": "prompt", "label": "工具清单 promptCatalog()",
			"detail": strings.TrimSpace(a.registry.PromptCatalog())})
		return fmt.Sprintf("当前注册表中共有 %d 个工具：\n%s\n这份清单就是模型每次对话时在系统提示词里「看见」的内容 —— 注册即生效，卸载即消失。",
			a.registry.Size(), a.registry.PromptCatalog())
	}

	// ---- 本地指令 2：卸载工具（演示注销器） ----
	if strings.HasPrefix(input, "卸载：") || strings.HasPrefix(input, "卸载:") {
		name := strings.TrimSpace(string([]rune(input)[3:]))
		d := a.disposers[name]
		if d == nil {
			return "工具 " + name + " 不在注册表中，无法卸载。当前清单：\n" + a.registry.PromptCatalog()
		}
		d()
		delete(a.disposers, name)
		a.lastTrace = append(a.lastTrace, map[string]any{
			"type": "guard", "label": "注销器执行 disposer.run()",
			"detail": fmt.Sprintf("工具 %s 已从注册表摘除，剩余 %d 个", name, a.registry.Size())})
		return "已卸载工具 " + name + "。注销器（disposer）就是注册时返回的 Runnable —— " +
			"插件停止/卸载时，宿主正是靠它回收工具、系统提示词等注册项。\n当前清单：\n" + a.registry.PromptCatalog() +
			"\n现在再问相关问题，模型将「看不见」这个工具。"
	}

	// ---- 常规对话：清单进提示词 → 模型决策 → 注册表执行 ----
	messages := []llm.Message{
		llm.System(systemPromptPrefix + a.registry.PromptCatalog() +
			"\n决策规则：\n- 需要工具时，只输出 JSON：{\"tool\": \"工具名\", \"arguments\": {\"参数名\": \"值\"}}\n" +
			"- 不需要工具时，直接用自然语言回答，不要输出 JSON。"),
		llm.User(input),
	}

	// 最多 2 轮：工具调用 → 结果回填 → 最终回答（与 Step04 同构，但执行走注册表）
	for round := 1; round <= 2; round++ {
		raw, err := llm.SafeChat(a.model, messages)
		if err != nil {
			raw = err.Error()
		}
		decision := llm.TryParse(strings.TrimSpace(raw))

		if decision != nil && llm.Str(decision, "tool") != "" {
			toolName := llm.Str(decision, "tool")
			a.lastTrace = append(a.lastTrace, map[string]any{
				"type": "action", "label": "模型发起工具调用",
				"detail": toolName + " " + llm.Write(decision["arguments"])})

			// 核心差异：执行器不认识任何具体工具，只查注册表
			tool := a.registry.Lookup(toolName)
			var result string
			if tool == nil {
				result = "未知工具: " + toolName + "。可用清单：\n" + a.registry.PromptCatalog()
				a.lastTrace = append(a.lastTrace, map[string]any{
					"type": "guard", "label": "注册表未命中",
					"detail": "模型只能调用注册表里存在的工具 —— 卸载后的工具自然不可调用"})
			} else {
				argsMap, _ := decision["arguments"].(map[string]any)
				if argsMap == nil {
					argsMap = map[string]any{}
				}
				out, execErr := tool.Execute(argsMap)
				if execErr != nil {
					result = "工具执行失败: " + execErr.Error()
				} else {
					result = out
				}
				a.lastTrace = append(a.lastTrace, map[string]any{
					"type": "observation", "label": "注册表执行 " + toolName, "detail": result})
			}

			messages = append(messages, llm.Assistant(raw))
			messages = append(messages, llm.User("工具 "+toolName+" 执行结果: "+result))
			continue
		}

		// 模型直接给出自然语言回答
		a.lastTrace = append(a.lastTrace, map[string]any{
			"type": "final", "label": "最终回答", "detail": "模型基于注册表提供的工具完成决策"})
		return raw
	}
	return "已达最大轮数，循环终止。"
}

func (a *Impl) Trace() []map[string]any {
	return agent.CopyTrace(a.lastTrace)
}

func (a *Impl) Reset() {
	a.registry.Clear()
	a.disposers = map[string]func(){}
	a.registerDefaults()
}

// ------------------------------------------------------------------ 内置工具：三个 ToolDefinition 实现

// WeatherTool 天气查询工具：实现协议四件套，即插即用。
type WeatherTool struct{}

var weatherData = map[string]string{
	"北京": "晴，26℃", "上海": "多云，28℃", "广州": "雷阵雨，31℃",
	"深圳": "阵雨，30℃", "杭州": "晴，27℃", "成都": "阴，22℃",
}

func (t *WeatherTool) Name() string        { return "get_weather" }
func (t *WeatherTool) Description() string { return "查询指定城市的实时天气" }
func (t *WeatherTool) Parameters() map[string]string {
	return map[string]string{"city": "string，城市名，如 北京"}
}
func (t *WeatherTool) Execute(args map[string]any) (string, error) {
	city := strings.TrimSpace(fmt.Sprintf("%v", argsOr(args, "city", "")))
	if v, ok := weatherData[city]; ok {
		return v, nil
	}
	return "未收录城市 " + city + " 的数据", nil
}
func (t *WeatherTool) ConcurrencySafe() bool { return false }

// CalculatorTool 四则运算工具。
type CalculatorTool struct{}

func (t *CalculatorTool) Name() string        { return "calculator" }
func (t *CalculatorTool) Description() string { return "计算四则运算，如 12*8" }
func (t *CalculatorTool) Parameters() map[string]string {
	return map[string]string{"expression": "string，算式，如 12*8"}
}
func (t *CalculatorTool) Execute(args map[string]any) (string, error) {
	expr := fmt.Sprintf("%v", argsOr(args, "expression", ""))
	m := calcRe.FindStringSubmatch(expr)
	if m == nil {
		return "无法解析表达式", nil
	}
	aNum, _ := strconv.ParseFloat(m[1], 64)
	bNum, _ := strconv.ParseFloat(m[3], 64)
	switch m[2] {
	case "+":
		return llm.FormatDouble(aNum + bNum), nil
	case "-":
		return llm.FormatDouble(aNum - bNum), nil
	case "*":
		return llm.FormatDouble(aNum * bNum), nil
	case "/":
		if bNum == 0 {
			return "除数不能为 0", nil
		}
		return llm.FormatDouble(aNum / bNum), nil
	default:
		return "不支持的运算符", nil
	}
}
func (t *CalculatorTool) ConcurrencySafe() bool { return false }

// TimeTool 时间工具。
type TimeTool struct{}

func (t *TimeTool) Name() string        { return "get_time" }
func (t *TimeTool) Description() string { return "获取当前时间" }
func (t *TimeTool) Parameters() map[string]string {
	return map[string]string{}
}
func (t *TimeTool) Execute(args map[string]any) (string, error) {
	return time.Now().Format("2006-01-02 15:04:05"), nil
}
func (t *TimeTool) ConcurrencySafe() bool { return false }

// calcRe 对齐 Java matches() 全串匹配语义。
var calcRe = regexp.MustCompile(`^\s*(\d+(?:\.\d+)?)\s*([+\-*/])\s*(\d+(?:\.\d+)?)\s*$`)

// argsOr 取参数，缺失回默认值（等价 Java args.getOrDefault）。
func argsOr(args map[string]any, key string, def any) any {
	if v, ok := args[key]; ok && v != nil {
		return v
	}
	return def
}
