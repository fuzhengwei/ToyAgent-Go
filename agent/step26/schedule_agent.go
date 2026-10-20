// Step26 · 定时工具 —— 给智能体装上「时间维度」的行动能力。
//
// 之前的工具都是即时的：调用 → 执行 → 返回。定时工具把「什么时候做」
// 也变成模型的决策对象：
//   - 一次性调度：n 秒后执行（time.AfterFunc 语义）；
//   - 循环调度：每 n 秒执行一次（等价 scheduleAtFixedRate，首延迟 = 周期）；
//   - 任务管理：列出 / 取消，每个任务有 job_id。
//
// 关键工程点：HTTP 请求已经返回，任务却在未来执行 —— 所以触发结果不能塞进
// 本次响应，要落盘（events/step26-tasks.jsonl，append-only 事件溯源，呼应 Step22）
// 并在后续对话中汇报。这也是生产提醒/巡检类 Agent 的最小雏形。
//
// 试试本场景的对话：
//
//	定时：5秒 提醒我喝水     —— 注册一次性任务，拿到 job_id
//	任务列表                 —— 看到 pending；5 秒后再问就变 done
//	循环：每10秒 报一次时     —— 周期任务，触发次数持续增长
//	取消：job-2              —— 摘掉循环任务
//	完成了什么               —— 汇报已触发任务（结果从事件日志读回）
package step26

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fuzhengwei/ToyAgent-Go/agent"
	"github.com/fuzhengwei/ToyAgent-Go/llm"
)

const systemPrompt = `你是 ToyAgent 的定时工具讲解员。用户可能询问定时任务、调度、提醒、巡检等话题。
只输出 JSON：{"final": "回答"}。请引导用户试用：
「定时：5秒 提醒我喝水」「循环：每10秒 报一次时」「任务列表」「取消：job-1」「完成了什么」。`

// eventFile 触发结果落盘文件（append-only JSONL，相对运行时 CWD）。
const eventFile = "events/step26-tasks.jsonl"

var (
	onceRe = regexp.MustCompile(`^定时：(\d+)\s*秒\s*(.*)$`)
	loopRe = regexp.MustCompile(`^循环：每(\d+)\s*秒\s*(.*)$`)
)

// job 定时任务：调度由独立 goroutine 承载（对应 Java ScheduledExecutorService），
// stop 通道用于取消，done 表达「已触发或已取消」（对应 Java future.isDone）。
// fired 计数与 Java 版保持一致（仅在循环任务列表展示，注册后不主动累加）。
type job struct {
	id      string
	typ     string
	content string
	period  int64
	stop    chan struct{}
	done    atomic.Bool
	fired   atomic.Int64
}

// Impl 定时工具实现。
type Impl struct {
	model      llm.ChatModel
	lastTrace  []map[string]any
	mu         sync.Mutex
	jobs       map[string]*job
	jobOrder   []string
	firedLogMu sync.Mutex
	firedLog   []string
	seq        atomic.Int64
}

// New 构造 Step26 智能体。
func New(model llm.ChatModel) agent.Agent {
	return &Impl{model: model, jobs: map[string]*job{}}
}

func (a *Impl) Name() string {
	return "Step26 · 定时工具（一次性/循环调度 · 触发结果事件落盘）"
}

func (a *Impl) Chat(input string) string {
	a.lastTrace = nil

	// —— 定时指令（宿主直连路由，不经过模型） ——
	if m := onceRe.FindStringSubmatch(input); m != nil {
		seconds, _ := strconv.ParseInt(m[1], 10, 64)
		return a.scheduleOnce(seconds, strings.TrimSpace(m[2]))
	}
	if m := loopRe.FindStringSubmatch(input); m != nil {
		seconds, _ := strconv.ParseInt(m[1], 10, 64)
		return a.scheduleRecurring(seconds, strings.TrimSpace(m[2]))
	}
	if strings.Contains(input, "任务列表") {
		return a.listJobs()
	}
	if strings.HasPrefix(input, "取消：") || strings.HasPrefix(input, "取消:") {
		return a.cancel(strings.TrimSpace(string([]rune(input)[3:])))
	}
	if strings.Contains(input, "完成了什么") || strings.Contains(input, "已触发") {
		return a.reportFired()
	}

	// —— 普通对话 → 模型讲解 ——
	raw, err := llm.SafeChat(a.model, []llm.Message{llm.System(systemPrompt), llm.User(input)})
	if err != nil {
		raw = ""
	}
	answer := raw
	if parsed := llm.TryParse(strings.TrimSpace(raw)); parsed != nil {
		if fin := llm.Str(parsed, "final"); fin != "" {
			answer = fin
		}
	}
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "final", "label": "讲解回答",
		"detail": "试试「定时：5秒 提醒我喝水」「循环：每10秒 报一次时」「任务列表」「完成了什么」"})
	return answer
}

// scheduleOnce 一次性调度：到点执行 → 落盘事件。
func (a *Impl) scheduleOnce(delaySeconds int64, content string) string {
	id := fmt.Sprintf("job-%d", a.seq.Add(1))
	j := &job{id: id, typ: "一次性", content: content, period: delaySeconds, stop: make(chan struct{})}
	a.mu.Lock()
	a.jobs[id] = j
	a.jobOrder = append(a.jobOrder, id)
	a.mu.Unlock()

	go a.runJob(j, delaySeconds, true)

	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "action", "label": "schedule_task · 一次性注册",
		"detail": fmt.Sprintf("%s：%d 秒后执行「%s」", id, delaySeconds, content)})
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "thought", "label": "异步语义",
		"detail": "本次调用立即返回；任务在未来由调度 goroutine 执行，结果落盘 events/step26-tasks.jsonl"})
	return fmt.Sprintf("⏰ 已注册一次性任务 %s：%d 秒后「%s」\n"+
		"本次请求已返回，任务到点后在后台执行并记录 —— 稍后问「完成了什么」看结果。", id, delaySeconds, content)
}

// scheduleRecurring 循环调度（首延迟 = 周期），触发次数持续累计。
func (a *Impl) scheduleRecurring(intervalSeconds int64, content string) string {
	id := fmt.Sprintf("job-%d", a.seq.Add(1))
	j := &job{id: id, typ: "循环", content: content, period: intervalSeconds, stop: make(chan struct{})}
	a.mu.Lock()
	a.jobs[id] = j
	a.jobOrder = append(a.jobOrder, id)
	a.mu.Unlock()

	go a.runJob(j, intervalSeconds, false)

	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "action", "label": "schedule_recurring · 循环注册",
		"detail": fmt.Sprintf("%s：每 %d 秒执行「%s」（首次延迟 = 周期）", id, intervalSeconds, content)})
	return fmt.Sprintf("🔁 已注册循环任务 %s：每 %d 秒「%s」\n"+
		"问「任务列表」看触发次数，问「完成了什么」看已触发内容，不需要时「取消：%s」。", id, intervalSeconds, content, id)
}

// runJob 调度循环：延迟/周期触发（等价 Java schedule / scheduleAtFixedRate）。
func (a *Impl) runJob(j *job, delay int64, once bool) {
	timer := time.NewTimer(time.Duration(delay) * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-timer.C:
		case <-j.stop:
			return
		}
		a.fire(j.id, j.content)
		j.done.Store(true) // 对应 Java future.isDone()：一次性任务触发后即 done
		if once {
			return
		}
		timer.Reset(time.Duration(j.period) * time.Second)
	}
}

// fire 调度回调：记录 + 落盘（append-only JSONL，事件溯源）。
func (a *Impl) fire(id, content string) {
	t := time.Now().Format("15:04:05")
	a.firedLogMu.Lock()
	a.firedLog = append(a.firedLog, t+"  ["+id+"] "+content)
	a.firedLogMu.Unlock()

	_ = os.MkdirAll(filepath.Dir(eventFile), 0o755)
	f, err := os.OpenFile(eventFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	// 与 Java 一致：手写 JSON 行（教学从简，不做转义）
	_, _ = f.WriteString(fmt.Sprintf("{\"time\":\"%s\",\"job\":\"%s\",\"content\":\"%s\"}\n", t, id, content))
}

func (a *Impl) listJobs() string {
	var sb strings.Builder
	sb.WriteString("📋 定时任务：\n")
	a.mu.Lock()
	jobs := make([]*job, 0, len(a.jobOrder))
	for _, id := range a.jobOrder {
		if j, ok := a.jobs[id]; ok {
			jobs = append(jobs, j)
		}
	}
	a.mu.Unlock()
	for _, j := range jobs {
		status := "pending"
		if j.done.Load() {
			status = "done/取消"
		} else if j.typ == "循环" {
			status = fmt.Sprintf("循环中 · 已触发 %d 次", j.fired.Load())
		}
		sb.WriteString(fmt.Sprintf("  %s · %s · 每/%d秒 · 「%s」 · %s\n", j.id, j.typ, j.period, j.content, status))
	}
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "observation", "label": "任务列表",
		"detail": fmt.Sprintf("%d 个任务在册", len(jobs))})
	return strings.TrimRight(sb.String(), "\n")
}

func (a *Impl) cancel(id string) string {
	a.mu.Lock()
	j := a.jobs[id]
	a.mu.Unlock()
	if j == nil {
		return "没有任务 " + id + "。在册：" + a.jobIds() 
	}
	if !j.done.Load() {
		close(j.stop)
		j.done.Store(true)
	}
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "guard", "label": "cancel_task · 取消",
		"detail": id + " 已停止（在册保留，状态 done/取消）"})
	return fmt.Sprintf("🚫 已取消 %s（「%s」不再触发）。", id, j.content)
}

func (a *Impl) reportFired() string {
	a.firedLogMu.Lock()
	log := append([]string(nil), a.firedLog...)
	a.firedLogMu.Unlock()
	detail := "暂无触发记录"
	if len(log) > 0 {
		detail = fmt.Sprintf("%d 条，事件已 append 到 %s", len(log), eventFile)
	}
	a.lastTrace = append(a.lastTrace, map[string]any{
		"type": "observation", "label": "已触发任务汇报", "detail": detail})
	if len(log) == 0 {
		return "✅ 已触发的任务（事件日志 " + eventFile + "，append-only）：\n（暂无 —— 注册一个「定时：5秒 提醒我喝水」再来看看）"
	}
	return "✅ 已触发的任务（事件日志 " + eventFile + "，append-only）：\n" + strings.Join(log, "\n")
}

// jobIds 在册任务 id（保持注册顺序，等价 Java keySet 展示）。
func (a *Impl) jobIds() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return "[" + strings.Join(a.jobOrder, ", ") + "]"
}

func (a *Impl) Trace() []map[string]any {
	return agent.CopyTrace(a.lastTrace)
}

func (a *Impl) Reset() {
	a.mu.Lock()
	for _, j := range a.jobs {
		if !j.done.Load() {
			close(j.stop)
			j.done.Store(true)
		}
	}
	a.jobs = map[string]*job{}
	a.jobOrder = nil
	a.mu.Unlock()
	a.firedLogMu.Lock()
	a.firedLog = nil
	a.firedLogMu.Unlock()
}
