// Step22 · 事件溯源 —— 会话即日志，日志即状态。
//
// 教学版拆出三件套：
//  1. 事件追加：会话中发生的每件事（用户发言/Agent 回答/回合完成）
//     都是一条不可变事件，append 到 JSONL 文件，永不改写历史；
//  2. 回放投影：每次对话前从日志重放全部事件，重建出会话上下文 ——
//     Agent 的「记忆」不是内存里的切片，而是从日志投影出来的视图；
//     服务重启后状态从日志完整恢复（试试：聊两轮 → 重启 → 还记得你名字）；
//  3. 审计能力：因为历史=事件流，天然获得审计、时间旅行、fork 会话等
//     衍生能力（子代理 fork 就是复制事件前缀）。
//
// 事件日志在 events/step22-events.jsonl，可直接打开查看每一条。
package step22

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fuzhengwei/ToyAgent-Go/agent"
	"github.com/fuzhengwei/ToyAgent-Go/llm"
)

// 事件类型（教学子集）。
const (
	evSession = "SESSION_STARTED"
	evUser    = "USER_MESSAGE_APPENDED"
	evAgent   = "AGENT_REPLY_APPENDED"
	evTurn    = "TURN_COMPLETED"
)

const systemPrompt = `你是一个跑在事件溯源架构上的智能体：你的每次对话都会作为事件写入 JSONL 日志，
每轮开始前从日志回放重建记忆。像正常助手一样回答即可（记住用户告诉你的信息）。
只输出 JSON：{"final": "回答"}。`

// logFile 事件日志路径（相对运行时 CWD，与 Java 一致）。
const logFile = "events/step22-events.jsonl"

// Impl 事件溯源实现：无内存状态字段 —— 记忆完全从日志投影。
type Impl struct {
	model     llm.ChatModel
	lastTrace []map[string]any
}

// New 构造 Step22 智能体。
func New(model llm.ChatModel) agent.Agent {
	return &Impl{model: model}
}

func (a *Impl) Name() string {
	return "Step22 · 事件溯源（JSONL 事件日志 + 回放投影重建会话）"
}

func (a *Impl) Chat(input string) string {
	a.lastTrace = nil

	// ===== 1. 回放：从日志重建会话状态 =====
	log, err := a.replay()
	if err != nil {
		return "事件日志读写失败：" + err.Error()
	}
	emptyNote := ""
	if len(log) == 0 {
		emptyNote = "（空日志 → 新会话）"
	}
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "thought", "label": "事件回放投影",
		"detail": fmt.Sprintf("从 %s 重放 %d 条事件，重建会话上下文%s", logFile, len(log), emptyNote)})

	// ===== 2. 追加本轮事件 =====
	if len(log) == 0 {
		if err := a.append(evSession, map[string]any{"scenario": "step22"}); err != nil {
			return "事件日志读写失败：" + err.Error()
		}
	}
	if err := a.append(evUser, map[string]any{"content": input}); err != nil {
		return "事件日志读写失败：" + err.Error()
	}

	// ===== 3. 上下文 = 投影结果（不是内存切片！）=====
	fresh, err := a.replay()
	if err != nil {
		return "事件日志读写失败：" + err.Error()
	}
	context := project(fresh)
	req := []llm.Message{llm.System(fmt.Sprintf("%s（当前事件日志共 %d 条）", systemPrompt, len(log)))}
	req = append(req, context...)

	raw, err := llm.SafeChat(a.model, req)
	if err != nil {
		raw = ""
	}
	answer := raw
	if parsed := llm.TryParse(strings.TrimSpace(raw)); parsed != nil {
		answer = orRaw(llm.Str(parsed, "final"), raw)
	}

	if err := a.append(evAgent, map[string]any{"content": answer}); err != nil {
		return "事件日志读写失败：" + err.Error()
	}
	if err := a.append(evTurn, map[string]any{"events": len(log) + 3}); err != nil {
		return "事件日志读写失败：" + err.Error()
	}
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "final", "label": "本轮事件已落盘",
		"detail": "追加 4 条事件（含会话首条）：USER → AGENT_REPLY → TURN_COMPLETED。" +
			"日志 append-only 永不改写，历史即事件流"})
	return answer
}

// replay 回放：逐行读 JSONL（append-only，所以重放顺序 = 发生顺序）。
func (a *Impl) replay() ([]map[string]any, error) {
	if err := ensureFile(); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(logFile)
	if err != nil {
		return nil, err
	}
	var logEntries []map[string]any
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var e map[string]any
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			// 损坏行跳过 —— append-only + 校验和是生产标配，教学从简
			continue
		}
		logEntries = append(logEntries, e)
	}
	return logEntries, nil
}

// project 投影：事件流 → 会话上下文（只挑 USER / AGENT 两类，其余事件服务审计）。
func project(logEntries []map[string]any) []llm.Message {
	var history []llm.Message
	for _, e := range logEntries {
		typ := fmt.Sprintf("%v", e["type"])
		payload, _ := e["payload"].(map[string]any)
		if payload == nil {
			continue
		}
		content := fmt.Sprintf("%v", payload["content"])
		switch typ {
		case evUser:
			history = append(history, llm.User(content))
		case evAgent:
			history = append(history, llm.Assistant(content))
		default:
			// SESSION_STARTED / TURN_COMPLETED 不进上下文，但留在日志里
		}
	}
	return history
}

// append 追加一条事件：seq 单调递增，一行一条 JSON。
func (a *Impl) append(typ string, payload map[string]any) error {
	if err := ensureFile(); err != nil {
		return err
	}
	lines := 0
	data, err := os.ReadFile(logFile)
	if err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.TrimSpace(line) != "" {
				lines++
			}
		}
	}
	seq := lines + 1
	event := map[string]any{
		"seq":     seq,
		"ts":      time.Now().UnixMilli(),
		"type":    typ,
		"payload": payload,
	}
	b, _ := json.Marshal(event)
	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(b, '\n'))
	return err
}

func ensureFile() error {
	if _, err := os.Stat(logFile); os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(logFile), 0o755); err != nil {
			return err
		}
		f, err := os.Create(logFile)
		if err != nil {
			return err
		}
		f.Close()
	}
	return nil
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

// Reset 删掉日志，记忆归零（对应 Java deleteIfExists）。
func (a *Impl) Reset() {
	a.lastTrace = nil
	_ = os.Remove(logFile)
}
