package llm

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// Models 模型工厂：读取配置，产出 ChatModel。
//
// 配置优先级：项目根目录 config.properties（非空值）> 环境变量 > 内置默认。
// 未配置 api-key 时 OpenAiOrNull() 返回 nil，由上层为每个场景装配 MockChatModel。
// 与 Java 版一致：不含 update()/save()/useMock()（那是 web 页面专用）。
var (
	confBaseUrl   string
	confApiKey    string
	confModelName string
)

func init() {
	load()
}

func load() {
	props := readProperties("config.properties")
	// 优先级：config.properties（非空值）> LLM_* 环境变量 > OPENAI_* 环境变量 > 内置默认
	// 文件里留空（api-key=）不遮蔽环境变量 —— 支持纯环境变量方式配置默认模型
	confBaseUrl = firstNonBlank(
		props["base-url"],
		env("LLM_BASE_URL"), env("OPENAI_BASE_URL"),
		"https://api.deepseek.com/v1")
	confApiKey = firstNonBlank(
		props["api-key"],
		env("LLM_API_KEY"), env("OPENAI_API_KEY"),
		"")
	confModelName = firstNonBlank(
		props["model"],
		env("LLM_MODEL"), env("OPENAI_MODEL"),
		"deepseek-chat")
	keyFromFile := strings.TrimSpace(props["api-key"]) != ""
	if strings.TrimSpace(confApiKey) != "" {
		from := "（Key 来自环境变量）"
		if keyFromFile {
			from = "（Key 来自 config.properties）"
		}
		fmt.Printf("[ToyAgent] 默认模型: %s @ %s%s\n", confModelName, confBaseUrl, from)
	}
}

// readProperties 解析 Java properties 格式（key=value，# / ! 为注释行）。
func readProperties(path string) map[string]string {
	props := map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		return props
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}
		idx := strings.IndexAny(line, "=:")
		if idx <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		props[key] = val
	}
	return props
}

// firstNonBlank 返回第一个非空值；全空则返回最后一个（内置默认）。
func firstNonBlank(vals ...string) string {
	for i := 0; i < len(vals)-1; i++ {
		if strings.TrimSpace(vals[i]) != "" {
			return vals[i]
		}
	}
	return vals[len(vals)-1]
}

func env(key string) string {
	return os.Getenv(key)
}

// OpenAiOrNull 有真实 Key 则返回 OpenAI 兼容模型，否则返回 nil（使用 Mock）。
func OpenAiOrNull() ChatModel {
	if strings.TrimSpace(confApiKey) == "" {
		return nil
	}
	return NewOpenAiChatModel(confBaseUrl, confApiKey, confModelName)
}

// Mock 教学演示用 Mock 模型。
func Mock(scenario string) ChatModel {
	return NewMockChatModel(scenario)
}

// IsRealModel 是否配置了真实模型 Key。
func IsRealModel() bool {
	return strings.TrimSpace(confApiKey) != ""
}

// ModelName 当前生效的模型名（Mock 模式返回 mock-model）。
func ModelName() string {
	if IsRealModel() {
		return confModelName
	}
	return "mock-model"
}

// BaseURL 当前配置的接口地址。
func BaseURL() string {
	return confBaseUrl
}
