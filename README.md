# ToyAgent-Go

> 用最简单的方式，讲清楚智能体实现 —— **一个接口，就是一个智能体的最小 MVP**。

Go 1.23 · 零第三方依赖（纯标准库）· 26 个渐进式场景 · CLI 演示运行器（web 版见 Java 工程 [ToyAgent](https://github.com/fuzhengwei/ToyAgent)）。本工程是 Java 版 ToyAgent 的 Go 完整移植，配套教程：[AI Agent 通识教程（ai-agent-guide）](../README.md)。

## 一句话读懂

智能体的灵魂只有一个方法：

```go
type Agent interface {
    Chat(input string) string   // 输入一句话，输出一句话
}
```

26 个场景按七条主题弧线递进：基础内核（1-4）→ 记忆与知识（5-8，含 RAG 与 LLM-Wiki）→ 工具的进化（9-11）→ 协作与派遣（12-14）→ 运行时与工程化（15-18，第 18 个全流程综合）→ 人机协同与安全（19-22）→ 扩展与形态（23-26）。

## 26 个场景

| # | 场景 | 包 | 新增能力 | 对应教程章节 |
|:---:|------|--------|----------|:---:|
| 01 | 对话智能体 | `step01` | 最小 MVP：接通模型 | ch01-03 |
| 02 | 提示词工程 | `step02` | 系统提示词三层结构 | ch04 |
| 03 | ReAct 循环 | `step03` | 思考-行动-观察 | ch04b/05 |
| 04 | 工具调用 | `step04` | Function Calling | ch03/10 |
| 05 | 记忆系统 | `step05` | 多轮记忆 + 上下文压缩 | ch06 |
| 06 | 意图路由 | `step06` | 意图识别 + 决策中枢 | ch06b/07 |
| 09 | MCP 协议 | `step09` | 工具标准化接口 | ch07b/11 |
| 10 | 技能编排 | `step10` | Skills L0/L1/L2 | ch08/12 |
| 07 | RAG | `step07` | 检索增强生成 | ch16/20 |
| 12 | 多智能体 | `step12` | 规划-执行-审查协作 | ch10/14 |
| 15 | Loop 运行时 | `step15` | 守卫 + 保险丝 + 兜底 | ch08b/18/21 |
| 16 | 工作流状态机 | `step16` | LangGraph 思想编排 | ch11b/15 |
| 18 | 全流程智能体 | `step18` | 守卫+记忆+ReAct+工具+保险丝 综合链路 | 综合 Step01-17 |
| 08 | LLM-Wiki 知识编译 | `step08` | 知识编译一次、持久维护、增量沉淀 | ch13/26 |
| 11 | 工具注册表 | `step11` | ToolDefinition 协议：注册/发现/热注销（disposer） | dsh-java |
| 17 | ReAct 运行时 | `step17` | turn/step 两级循环 + 上下文裁剪 + TurnEndReason | dsh-java |
| 19 | 人工介入 | `step19` | ask_user_question：提问挂起 → 人工答复续跑 | dsh-java |
| 20 | 审批门禁 | `step20` | 风险分级 + 审批挂起 + 会话免审 + 超时默认 DENY | dsh-java |
| 21 | 沙箱纵深防御 | `step21` | 策略/黑名单/边界/规范化 四层拦截 | dsh-java |
| 22 | 事件溯源 | `step22` | JSONL 事件流 append-only + 回放投影重建状态 | dsh-java |
| 23 | 插件机制 | `step23` | AgentPlugin 契约 + 注册发现装载（Go 无运行时 ClassLoader，见场景内注释） | dsh-java |
| 25 | CLI 智能体 | `step25` | 终端会话 + 沙箱 shell 执行器（复用 Step21 四层防御） | 教程 ch18 |
| 13 | 子代理 | `step13` | dispatch_subagent：spawn 全新上下文 / fork 继承父会话 | dsh-java |
| 24 | Hooks 钩子 | `step24` | before 拦截/改写参数 · after 脱敏/审计，挂钩即生效摘钩即消失 | dsh-java |
| 14 | A2A 协作 | `step14` | Agent Card 名片发现 + task_id 信封 + 异步回执 | dsh-java |
| 26 | 定时工具 | `step26` | 一次性/循环调度，触发结果 append 到事件日志可回看 | dsh-java |

（清单顺序即 CLI `list` 输出顺序，与 Java Application 注册顺序一致的教学叙事顺序。）

每个场景一个包，位于 `agent/stepXX/`，包顶部注释即教程，`Impl` 即全部逻辑。

## 快速开始

```bash
cd ToyAgent-Go

# 直接运行（交互 REPL）
go run .

# 或编译后运行
go build -o toyagent .
./toyagent
```

常用命令：

```bash
./toyagent                      # 交互 REPL：先选场景，再循环输入（/reset 清状态，/exit 退出）
./toyagent list                 # 26 个场景清单
./toyagent help                 # 帮助
./toyagent run step03 "北京天气怎么样"      # 单轮执行，打印回答 + 轨迹 + 耗时
./toyagent run step04 "3+5等于多少"
```

### 接入真实大模型（可选）

复制配置模板并填入任意 OpenAI 兼容 API（DeepSeek / 通义 / 智谱 / 火山方舟…）：

```properties
# config.properties
base-url=https://api.deepseek.com/v1
api-key=sk-xxx
model=deepseek-chat
```

也可用环境变量（优先级：config.properties 非空值 > `LLM_*` > `OPENAI_*` > 内置默认）：

```bash
export LLM_BASE_URL=https://api.deepseek.com/v1
export LLM_API_KEY=sk-xxx
export LLM_MODEL=deepseek-chat
```

> 未配置 Key 时自动使用内置 **Mock 模型**：不联网、不花钱，所有场景的执行轨迹（ReAct 循环、工具调用、路由决策、RAG 检索…）照常完整展示。配上真实 Key，同一套骨架立刻换上真大脑。

## 项目结构

```
ToyAgent-Go/
├── go.mod                              # module github.com/fuzhengwei/ToyAgent-Go（零第三方依赖）
├── main.go                             # CLI 运行器：REPL / list / run
├── config.properties.example           # 模型配置模板
├── sandbox/step25/hello.txt            # Step25 受限工作区预置文件
├── agent/
│   ├── agent.go                        # 智能体契约：Chat(input)
│   └── step01..step26/                 # 26 个场景，一场景一包，Impl 即全部逻辑
└── llm/                                # 模型层
    ├── chat_model.go                   # 模型抽象（大脑）
    ├── openai_chat_model.go            # OpenAI 兼容协议实现（net/http）
    ├── mock_chat_model.go              # 教学 Mock 模型（逐场景剧本 1:1）
    ├── models.go                       # 配置工厂
    ├── message.go                      # 对话消息
    └── json_helper.go                  # 基于标准库 encoding/json 的便捷封装
```

## 设计理念

1. **一个场景包 = 一个智能体 MVP** —— 拒绝框架魔法，所有逻辑看得见、摸得着；
2. **零依赖** —— 只用 Go 标准库（`net/http`、`encoding/json`、`regexp`、`time` 等），clone 即跑；
3. **由浅入深** —— 26 个场景按七条主题弧线严格递进，每个场景只新增一种能力，包顶部注释就是教程；
4. **Mock/真实双模式** —— 骨架与大脑分离，学习时用 Mock 观察轨迹，进阶时配 Key 换真模型；
5. **跨场景复用即组合** —— Step25 的 shell 执行器直接 import Step21 的沙箱校验，工具协议（Step11）被 Step17/23 共享，体现 Go 包模型的组合能力。

## 与 Java 版的差异说明

- 入口为 CLI 运行器（无 web 页面 / HTTP 服务）；trace 数据结构与场景行为保持一致；
- Go 无法运行时动态加载源码：Step23 插件以「契约接口 + 注册发现」模拟 Java 的独立 ClassLoader 装载语义（安装后工具出现、卸载后消失，行为一致）；
- Step25 在自有三层防御之后复用 Step21 的黑名单/边界/规范化校验作为纵深兜底（规范要求的跨场景复用），对极个别「过白名单但含特殊模式」的命令比 Java 版更严格；
- 正则细节：Go RE2 不支持负向前瞻/后顾，Step18 画像提取与 Step21 绝对路径匹配改为等价改写（行为一致，见代码注释）。

## License

Apache-2.0 © 小傅哥
