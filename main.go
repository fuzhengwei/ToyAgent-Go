// ToyAgent CLI 演示运行器（Go 版，替代 Java 版的 web 页面入口）。
//
// 命令：
//   toyagent            交互 REPL（先选场景，再循环输入；/reset 清状态、/exit 退出）
//   toyagent help       帮助 + 26 个场景清单
//   toyagent list       场景清单
//   toyagent run <stepId> "<message>"
//                       单轮执行指定场景，打印场景名、回答、执行轨迹时间线与耗时
//
// 场景注册顺序即 Java Application 里的教学叙事顺序。
package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/fuzhengwei/ToyAgent-Go/agent"
	"github.com/fuzhengwei/ToyAgent-Go/llm"
	step01 "github.com/fuzhengwei/ToyAgent-Go/agent/step01"
	step02 "github.com/fuzhengwei/ToyAgent-Go/agent/step02"
	step03 "github.com/fuzhengwei/ToyAgent-Go/agent/step03"
	step04 "github.com/fuzhengwei/ToyAgent-Go/agent/step04"
	step05 "github.com/fuzhengwei/ToyAgent-Go/agent/step05"
	step06 "github.com/fuzhengwei/ToyAgent-Go/agent/step06"
	step07 "github.com/fuzhengwei/ToyAgent-Go/agent/step07"
	step08 "github.com/fuzhengwei/ToyAgent-Go/agent/step08"
	step09 "github.com/fuzhengwei/ToyAgent-Go/agent/step09"
	step10 "github.com/fuzhengwei/ToyAgent-Go/agent/step10"
	step11 "github.com/fuzhengwei/ToyAgent-Go/agent/step11"
	step12 "github.com/fuzhengwei/ToyAgent-Go/agent/step12"
	step13 "github.com/fuzhengwei/ToyAgent-Go/agent/step13"
	step14 "github.com/fuzhengwei/ToyAgent-Go/agent/step14"
	step15 "github.com/fuzhengwei/ToyAgent-Go/agent/step15"
	step16 "github.com/fuzhengwei/ToyAgent-Go/agent/step16"
	step17 "github.com/fuzhengwei/ToyAgent-Go/agent/step17"
	step18 "github.com/fuzhengwei/ToyAgent-Go/agent/step18"
	step19 "github.com/fuzhengwei/ToyAgent-Go/agent/step19"
	step20 "github.com/fuzhengwei/ToyAgent-Go/agent/step20"
	step21 "github.com/fuzhengwei/ToyAgent-Go/agent/step21"
	step22 "github.com/fuzhengwei/ToyAgent-Go/agent/step22"
	step23 "github.com/fuzhengwei/ToyAgent-Go/agent/step23"
	step24 "github.com/fuzhengwei/ToyAgent-Go/agent/step24"
	step25 "github.com/fuzhengwei/ToyAgent-Go/agent/step25"
	step26 "github.com/fuzhengwei/ToyAgent-Go/agent/step26"
)

// scenario 场景注册项：stepId + 工厂（真实模型优先，无 Key 由工厂内部回落 Mock）。
type scenario struct {
	id    string
	newFn func(model llm.ChatModel) agent.Agent
}

// registry 场景工厂注册表，顺序 = Java Application 的注册顺序（教学叙事顺序）。
// 模型装配在 buildAgent 中完成：真实模型优先，无 Key 则回落 Mock（同 Java scoped()）。
func registry() []scenario {
	return []scenario{
		{"step01", step01.New}, {"step02", step02.New}, {"step03", step03.New},
		{"step04", step04.New}, {"step05", step05.New}, {"step06", step06.New},
		{"step09", step09.New}, {"step10", step10.New}, {"step07", step07.New},
		{"step12", step12.New}, {"step15", step15.New}, {"step16", step16.New},
		{"step18", step18.New}, {"step08", step08.New}, {"step11", step11.New},
		{"step17", step17.New}, {"step19", step19.New}, {"step20", step20.New},
		{"step21", step21.New}, {"step22", step22.New}, {"step23", step23.New},
		{"step25", step25.New}, {"step13", step13.New}, {"step24", step24.New},
		{"step14", step14.New}, {"step26", step26.New},
	}
}

// resolveModel 场景装配：真实模型优先，无 Key 则装配 Mock 模型（同 Java scoped()）。
func resolveModel(stepId string) llm.ChatModel {
	if real := llm.OpenAiOrNull(); real != nil {
		return real
	}
	return llm.Mock(stepId)
}

func buildAgent(s scenario) agent.Agent {
	return s.newFn(resolveModel(s.id))
}

func main() {
	args := os.Args[1:]
	printBanner()

	// 无参数 → 交互 REPL（先选场景，再循环输入）
	if len(args) == 0 {
		runRepl()
		return
	}

	switch args[0] {
	case "help", "-h", "--help":
		printHelp()
	case "list":
		printList()
	case "run":
		if len(args) < 2 {
			fmt.Println("用法: run <stepId> \"<message>\"   例: run step01 \"你好\"")
			os.Exit(1)
		}
		runOnce(args[1], strings.Join(args[2:], " "))
	default:
		fmt.Printf("未知命令: %s\n\n", args[0])
		printHelp()
		os.Exit(1)
	}
}

func printBanner() {
	mode := "Mock 模型（未配置 api-key）"
	if llm.IsRealModel() {
		mode = "真实模型 " + llm.ModelName()
	}
	fmt.Println("==============================================")
	fmt.Println("  ToyAgent v1.0 (Go)  CLI 演示运行器")
	fmt.Println("  模型模式: " + mode)
	fmt.Println("  输入 help 查看用法，list 查看场景清单")
	fmt.Println("==============================================")
}

func printHelp() {
	fmt.Println(`用法:
  toyagent                        交互 REPL：先选场景，再循环输入（/reset 清状态，/exit 退出）
  toyagent help                   打印本帮助与场景清单
  toyagent list                   场景清单
  toyagent run <stepId> "<msg>"   单轮执行，例: run step03 "北京天气怎么样"`)
	fmt.Println()
	printList()
}

func printList() {
	fmt.Println("26 个场景（编号即注册顺序，stepId 形如 step01）:")
	for i, s := range registry() {
		a := buildAgent(s)
		fmt.Printf("  %2d. %-7s %s\n", i+1, s.id, a.Name())
	}
}

// runOnce 单轮执行：打印场景名、回答、轨迹时间线、耗时。
func runOnce(stepId, message string) {
	sc, ok := findScenario(stepId)
	if !ok {
		fmt.Println("场景不存在: " + stepId + "（用 list 查看可用场景）")
		os.Exit(1)
	}
	a := buildAgent(sc)

	// 兜底：智能体内部未捕获的异常不拖垮 CLI（对应 Java Application 的 catch 500）
	answer, err := func() (ans string, err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("%v", r)
			}
		}()
		return a.Chat(message), nil
	}()
	if err != nil {
		fmt.Println("智能体执行失败: " + err.Error())
		os.Exit(1)
	}

	fmt.Println("【" + a.Name() + "】")
	fmt.Println("回答: " + answer)
	fmt.Println("执行轨迹:")
	printTrace(a.Trace())
}

// runRepl 交互模式：先选场景，再循环输入；/reset 清空状态、/exit 退出。
func runRepl() {
	printList()
	fmt.Println()
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var current scenario
	var currentAgent agent.Agent

	for {
		if current.id == "" {
			fmt.Print("请选择场景（输入编号 1-26 或 stepId，如 step03）> ")
			if !in.Scan() {
				return
			}
			line := strings.TrimSpace(in.Text())
			if line == "" {
				continue
			}
			sc, ok := pickScenario(line)
			if !ok {
				fmt.Println("无法识别的场景: " + line)
				continue
			}
			current = sc
			currentAgent = buildAgent(sc)
			fmt.Println("已选择: " + currentAgent.Name() + "（/reset 清状态，/exit 退出）")
			continue
		}

		fmt.Print("你> ")
		if !in.Scan() {
			return
		}
		line := strings.TrimSpace(in.Text())
		if line == "" {
			continue
		}
		switch line {
		case "/exit", "exit", "quit":
			fmt.Println("Bye.")
			return
		case "/reset":
			currentAgent.Reset()
			fmt.Println("已重置场景状态。")
			continue
		case "/scene", "/switch":
			current = scenario{}
			currentAgent = nil
			continue
		}

		start := time.Now()
		answer, err := func() (ans string, err error) {
			defer func() {
				if r := recover(); r != nil {
					err = fmt.Errorf("%v", r)
				}
			}()
			return currentAgent.Chat(line), nil
		}()
		if err != nil {
			fmt.Println("智能体执行失败: " + err.Error())
			continue
		}
		fmt.Println("Agent> " + answer)
		printTrace(currentAgent.Trace())
		fmt.Printf("（耗时 %dms）\n", time.Since(start).Milliseconds())
	}
}

func findScenario(stepId string) (scenario, bool) {
	for _, s := range registry() {
		if s.id == strings.ToLower(strings.TrimSpace(stepId)) {
			return s, true
		}
	}
	return scenario{}, false
}

// pickScenario 支持 "3"、"03"、"step03" 三种写法。
func pickScenario(line string) (scenario, bool) {
	if sc, ok := findScenario(line); ok {
		return sc, true
	}
	if n, err := strconv.Atoi(strings.TrimPrefix(line, "step")); err == nil {
		if n >= 1 && n <= 26 {
			return findScenario(fmt.Sprintf("step%02d", n))
		}
	}
	return scenario{}, false
}

// printTrace 轨迹时间线：[Thought 1] ... / [Action 1] ... / [Observation 1] ... / [Final] ...
func printTrace(tr []map[string]any) {
	for _, t := range tr {
		fmt.Printf("  [%v] %v\n", t["label"], t["detail"])
	}
}
