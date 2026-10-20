// Step23 · 插件机制 —— 把「一批工具」打包成可安装、可卸载的插件。
//
// 与 Step11 的关系：Step11 解决「工具怎么注册」，本步解决「工具从哪来」——
// 插件是工具的动态载体：安装插件 = 批量注册，卸载插件 = 批量调用 disposer。
//
// 四个关键设计：
//  1. 契约 SPI：AgentPlugin 接口（ID/OnStart/OnStop/Tools）；
//  2. 隔离加载：Java 版用独立 URLClassLoader 隔离插件类；Go 无法在运行时
//     动态加载源码/字节码 —— 本版用「插件契约接口 + 注册发现」模拟装载语义
//     （差异说明见 materializePluginPackage）；
//  3. 生命周期：install → onStart → 收集工具 → 批量注册 →（卸载）批量注销 → onStop；
//  4. 工具桥接：插件的 ToolDefinition 注册进 ToolRegistry，模型立即「看见」——
//     注册项的 disposer 按 pluginId 归档。
//
// 试试本场景的本地指令：
//
//	安装插件              —— 释放插件包 → 加载 → 批量注册 2 个工具
//	查看工具清单          —— 注册表当前内容（模型看到的清单）
//	掷一次硬币 / 运行多久了 —— 调用插件带来的工具
//	卸载插件              —— 批量注销 + onStop
//	再看看工具清单        —— 插件工具已消失，宿主回到零工具
package step23

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fuzhengwei/ToyAgent-Go/agent"
	"github.com/fuzhengwei/ToyAgent-Go/llm"
	"github.com/fuzhengwei/ToyAgent-Go/agent/step11"
)

// pluginDir 插件包落盘目录。
const pluginDir = "plugins/step23"

// systemPromptPrefix 系统提示词前缀。
const systemPromptPrefix = `你是一个可以调用工具的智能体。可用工具清单（由注册表自动生成，工具由插件提供）：
`

// AgentPlugin 插件契约：插件作者唯一需要实现的东西。
type AgentPlugin interface {
	// ID 插件唯一标识，卸载时靠它批量回收注册项。
	ID() string
	// Version 插件版本。
	Version() string
	// OnStart 生命周期：安装/激活后由宿主调用一次。
	OnStart()
	// OnStop 生命周期：卸载前由宿主调用一次，释放插件自有资源。
	OnStop()
	// Tools 插件带来的工具：宿主逐个注册进 ToolRegistry。
	Tools() []step11.ToolDefinition
}

// loaded 已安装插件与其回收资源。
type loaded struct {
	plugin    AgentPlugin
	toolNames []string
	disposers []func()
}

// Impl 插件机制实现。
type Impl struct {
	model     llm.ChatModel
	registry  *step11.ToolRegistry
	startedAt time.Time
	installed map[string]*loaded // 已安装插件：pluginId → 已加载实例与回收资源
	lastTrace []map[string]any
}

// New 构造 Step23 智能体。
func New(model llm.ChatModel) agent.Agent {
	return &Impl{
		model:     model,
		registry:  step11.NewToolRegistry(),
		startedAt: time.Now(),
		installed: map[string]*loaded{},
	}
}

func (a *Impl) Name() string {
	return "Step23 · 插件机制（JavaPluginLoader 隔离加载）"
}

func (a *Impl) Chat(input string) string {
	a.lastTrace = nil
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "tool", "label": "注册表就绪",
		"detail": fmt.Sprintf("宿主内置 0 个工具 —— 能力全部由插件装进来（已安装 %d 个插件）", len(a.installed))})

	// ---- 本地指令 1：安装插件 ----
	if strings.Contains(input, "安装插件") || strings.Contains(input, "装载插件") {
		return a.install()
	}

	// ---- 本地指令 2：卸载插件 ----
	if strings.Contains(input, "卸载插件") || strings.Contains(input, "移除插件") {
		return a.uninstall()
	}

	// ---- 本地指令 3：查看工具清单 ----
	if strings.Contains(input, "查看工具") || strings.Contains(input, "工具清单") {
		list := "（无）"
		if len(a.installed) > 0 {
			var ids []string
			for id := range a.installed {
				ids = append(ids, id)
			}
			list = strings.Join(ids, "、")
		}
		catalog := "（注册表为空）"
		if !a.registry.IsEmpty() {
			catalog = strings.TrimSpace(a.registry.PromptCatalog())
		}
		a.lastTrace = append(a.lastTrace, map[string]any{
			"type": "prompt", "label": "工具清单 promptCatalog()", "detail": catalog})
		catalogLine := "（空）"
		if !a.registry.IsEmpty() {
			catalogLine = a.registry.PromptCatalog()
		}
		return "已安装插件：" + list + "\n当前注册表共 " + fmt.Sprint(a.registry.Size()) + " 个工具：\n" +
			catalogLine +
			"\n插件注册的工具对模型完全透明 —— 它只看得到清单，不知道工具是内置的还是插件带来的。"
	}

	// ---- 常规对话：清单进提示词 → 模型决策 → 注册表执行 ----
	catalog := "（当前没有任何工具）"
	if !a.registry.IsEmpty() {
		catalog = a.registry.PromptCatalog()
	}
	messages := []llm.Message{
		llm.System(systemPromptPrefix + catalog +
			"\n决策规则：\n- 需要工具时，只输出 JSON：{\"tool\": \"工具名\", \"arguments\": {}}\n" +
			"- 不需要工具时，直接用自然语言回答，不要输出 JSON。"),
		llm.User(input),
	}

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

			tool := a.registry.Lookup(toolName)
			var result string
			if tool == nil {
				result = "未知工具: " + toolName + "（插件未安装，或已被卸载）"
				a.lastTrace = append(a.lastTrace, map[string]any{
					"type": "guard", "label": "注册表未命中",
					"detail": "卸载插件后其工具随之消失 —— 模型看不见，也调不到"})
			} else {
				out, execErr := tool.Execute(map[string]any{})
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

		a.lastTrace = append(a.lastTrace, map[string]any{
			"type": "final", "label": "最终回答", "detail": "模型基于插件注册的工具完成决策"})
		return raw
	}
	return "已达最大轮数，循环终止。"
}

// install 安装：释放插件包 → 加载 → onStart → 收集工具 → 批量注册。
func (a *Impl) install() string {
	if len(a.installed) > 0 {
		var ids []string
		for id := range a.installed {
			ids = append(ids, id)
		}
		return "已有插件在运行：" + strings.Join(ids, "、") + "。先卸载再安装其他插件。"
	}

	// 1. 释放插件包（真实场景：用户下载插件包放入 plugins/ 目录）
	if err := a.materializePluginPackage(); err != nil {
		return "插件包落盘失败：" + err.Error()
	}
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "tool", "label": "插件包落盘 " + pluginDir,
		"detail": "TimePlugin + plugin.properties（entrypoint 声明入口，等价 META-INF/plugin.yaml）"})

	// 2. 加载：Java 版用独立 URLClassLoader 隔离加载；Go 无运行时动态加载能力，
	//    按注册发现语义直接实例化插件（行为面一致：onStart → 工具出现）。
	plugin := &timePlugin{}
	plugin.OnStart()
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "action", "label": "插件加载（注册发现语义）",
		"detail": "入口 TimePlugin 实例化成功，onStart() 已调用 —— Go 版以契约接口+注册发现模拟隔离加载，卸载即停用"})

	// 3. 工具桥接：逐个注册，disposer 按 pluginId 归档
	l := &loaded{plugin: plugin}
	for _, tool := range plugin.Tools() {
		l.disposers = append(l.disposers, a.registry.Register(tool))
		l.toolNames = append(l.toolNames, tool.Name())
	}
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "observation", "label": "工具桥接 batch register",
		"detail": "[" + strings.Join(l.toolNames, ", ") + "] 已注册进 ToolRegistry，disposers 按 pluginId 归档 —— 卸载即批量注销"})

	a.installed[plugin.ID()] = l
	return fmt.Sprintf("插件 %s v%s 安装成功，带来 %d 个工具：\n%s\n现在可以直接对话使用（如「掷一次硬币」「运行多久了」），模型自动看见新工具。",
		plugin.ID(), plugin.Version(), len(l.toolNames), a.registry.PromptCatalog())
}

// uninstall 卸载：批量注销工具 → onStop。
func (a *Impl) uninstall() string {
	if len(a.installed) == 0 {
		return "当前没有已安装的插件。先试试「安装插件」。"
	}
	var sb strings.Builder
	for id, l := range a.installed {
		for _, d := range l.disposers {
			d()
		}
		l.plugin.OnStop()
		a.lastTrace = append(a.lastTrace, map[string]any{
			"type": "guard", "label": "卸载 " + id,
			"detail": "[" + strings.Join(l.toolNames, ", ") + "] 已批量注销，onStop() 已调用"})
		sb.WriteString("[" + strings.Join(l.toolNames, ", ") + "] ")
	}
	a.installed = map[string]*loaded{}
	return "插件已卸载，工具 " + sb.String() + "全部注销。注册表回到 " + fmt.Sprint(a.registry.Size()) +
		" 个工具 —— 插件机制的三步回收：批量 disposer → onStop → 释放插件实例。\n" +
		"现在再让模型掷硬币，它会说工具不存在 —— 卸载即消失。"
}

// materializePluginPackage 把演示插件清单释放到 plugins/ 目录，模拟「放入插件包」。
//
// 语言差异说明：Java 版在此把 3 个 .class 文件释放到 plugins/ 供 URLClassLoader
// 加载；Go 无法运行时动态加载源码/字节码，这里落盘 plugin.properties 清单
// 表达「插件包已就位」，真正的装载走上面的注册发现语义。
func (a *Impl) materializePluginPackage() error {
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		return err
	}
	manifest := "# mini plugin manifest (生产用 META-INF/plugin.yaml)\n" +
		"id=time-plugin\nversion=1.0.0\nentrypoint=github.com/fuzhengwei/ToyAgent-Go/agent/step23.timePlugin\n"
	return os.WriteFile(filepath.Join(pluginDir, "plugin.properties"), []byte(manifest), 0o644)
}

func (a *Impl) Trace() []map[string]any {
	return agent.CopyTrace(a.lastTrace)
}

func (a *Impl) Reset() {
	for _, l := range a.installed {
		for _, d := range l.disposers {
			d()
		}
		l.plugin.OnStop()
	}
	a.installed = map[string]*loaded{}
	a.registry.Clear()
}

// ------------------------------------------------------------------ 演示插件：安装后带来 2 个工具

// timePlugin 演示插件 —— 如果它有 main 函数的人生，这就是「插件作者」写的全部代码：
// 实现 AgentPlugin 契约，OnStart 打个招呼，Tools() 返回带来的工具。
// 安装时它经契约接口被宿主发现 —— Java 版则经独立 ClassLoader 隔离加载。
type timePlugin struct {
	started bool
}

func (p *timePlugin) ID() string      { return "time-plugin" }
func (p *timePlugin) Version() string { return "1.0.0" }
func (p *timePlugin) OnStart()        { p.started = true }
func (p *timePlugin) OnStop()         { p.started = false }
func (p *timePlugin) Tools() []step11.ToolDefinition {
	return []step11.ToolDefinition{&uptimeTool{startedAt: time.Now()}, &coinTool{}}
}

// uptimeTool 工具 1：宿主进程运行时长 —— 插件工具也能反查宿主状态。
type uptimeTool struct{ startedAt time.Time }

func (t *uptimeTool) Name() string        { return "uptime" }
func (t *uptimeTool) Description() string { return "查询宿主进程已运行时长" }
func (t *uptimeTool) Parameters() map[string]string {
	return map[string]string{}
}
func (t *uptimeTool) Execute(args map[string]any) (string, error) {
	sec := int64(time.Since(t.startedAt).Seconds())
	h, m, s := sec/3600, (sec%3600)/60, sec%60
	return fmt.Sprintf("宿主已运行 %d 小时 %d 分 %d 秒", h, m, s), nil
}
func (t *uptimeTool) ConcurrencySafe() bool { return false }

// coinTool 工具 2：掷硬币 —— 一个纯粹由插件带来的趣味能力。
type coinTool struct{}

func (t *coinTool) Name() string        { return "coin_flip" }
func (t *coinTool) Description() string { return "掷一次硬币，返回正面或反面" }
func (t *coinTool) Parameters() map[string]string {
	return map[string]string{}
}
func (t *coinTool) Execute(args map[string]any) (string, error) {
	if rand.Intn(2) == 0 {
		return "正面", nil
	}
	return "反面", nil
}
func (t *coinTool) ConcurrencySafe() bool { return false }
