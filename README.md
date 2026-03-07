# nanobot-go

[English](README.en.md) | 中文

NanoBot 的 Go 语言完整实现 — 一个轻量级的、支持工具调用的 AI Agent，兼容任何 OpenAI API 格式的 LLM 端点。

## ✨ 特性

| 状态 | 功能 |
|------|------|
| ✅ | OpenAI 兼容的 Provider（直接 HTTP 调用，无需 SDK） |
| ✅ | 工具系统 — shell 执行、文件读写/编辑/列表 |
| ✅ | ReAct Agent 循环，支持多轮会话历史 |
| ✅ | CLI 通道（标准输入/输出） |
| ✅ | Gateway 子命令（多通道服务器） |
| ✅ | Feishu/Lark 通道（WebSocket 长连接，无需公网 IP） |
| ✅ | 配置文件支持（JSON 格式） |
| ✅ | Memory / Session 持久化（磁盘存储） |
| ✅ | Cron / Heartbeat 服务 |
| ✅ | Skills 系统（渐进式加载，支持依赖检查） |
| ✅ | 系统提示词模板（AGENTS.md, SOUL.md, USER.md, TOOLS.md） |
| ✅ | 两层记忆系统（MEMORY.md + HISTORY.md） |
| ☐ | Docker 支持 |
| ☐ | Multi-agent 编排 |

---

## 📦 包结构

| 包 | 文件 | 功能 |
|----|------|------|
| `provider` | `interface.go` | 核心类型：`Message`, `LLMResponse`, `LLMProvider` 接口 |
| `provider` | `openai_compat.go` | 直接 HTTP 调用的 OpenAI 兼容 Provider |
| `bus` | `bus.go` | `MessageBus`：使用带缓冲的 Go channel 处理入站/出站消息 |
| `tool` | `tool.go` | `Tool` 接口 + `ToolRegistry`（注册/获取/执行） |
| `tool` | `shell.go` | `ShellTool`（"exec"）：运行 `sh -c` 命令，支持超时，捕获 stdout+stderr |
| `tool` | `filesystem.go` | 文件工具：`ReadFileTool`, `WriteFileTool`, `EditFileTool`, `ListDirTool` |
| `agent` | `session.go` | Session 管理：`SessionMessage`, `Session`, `SessionManager`（JSONL 持久化） |
| `agent` | `memory.go` | 记忆系统：`MemoryStore`（MEMORY.md + HISTORY.md） |
| `agent` | `skills.go` | Skills 加载器：渐进式加载，支持 YAML frontmatter 和依赖检查 |
| `agent` | `prompt.go` | 系统提示词构建器：核心身份 + bootstrap 文件 + skills + memory |
| `agent` | `context.go` | 消息构建辅助函数：`BuildMessages`, `AddAssistantMessage`, `AddToolResult` |
| `agent` | `loop.go` | `AgentLoop`：`Run(ctx)`, `ProcessMessage`, `runReActLoop` |
| `agent` | `init.go` | Workspace 初始化：创建目录和默认文件 |
| `channel` | `channel.go` | `Channel` 接口 + `BaseChannel`（白名单、`HandleMessage`） |
| `channel` | `cli.go` | `CLIChannel`：标准输入循环、spinner、`/new` `/exit` `/quit` 命令 |
| `channel` | `feishu.go` | `FeishuChannel`：通过 WebSocket 长连接接入飞书/Lark 机器人 |
| `cmd/nanobot-go` | `main.go` | 入口点：解析命令行参数，组装 provider + bus + agent + channel |
| `cmd/nanobot-go` | `gateway.go` | `gateway` 子命令：多通道服务器（CLI + Feishu） |

---

## 🏗️ 架构

```
┌──────────────┐     InboundMessage     ┌─────────────┐
│   Channel    │ ─────────────────────► │ MessageBus  │
│ (CLI / Feishu)│                        │  (buffered  │
│              │ ◄───────────────────── │   channels) │
└──────────────┘    OutboundMessage     └──────┬──────┘
                                               │
                                        ┌──────▼──────┐
                                        │  AgentLoop  │
                                        │  (ReAct)    │
                                        └──────┬──────┘
                                  Chat  │      │ ToolCall
                            ┌──────────┘      └──────────┐
                            ▼                             ▼
                     ┌─────────────┐             ┌──────────────┐
                     │ LLMProvider │             │ ToolRegistry │
                     │ (OpenAI-    │             │ (shell, fs)  │
                     │  compat)    │             └──────────────┘
                     └─────────────┘
```

各个 Channel 将用户消息作为 `InboundMessage` 发布到 bus 上。`AgentLoop` 消费这些消息，运行 ReAct 循环（LLM → 工具调用 → 观察结果 → LLM ……），然后将最终回复作为 `OutboundMessage` 发布。`main` 或 `gateway` 中的出站分发器 goroutine 读取出站消息，并根据通道名称路由回对应的 Channel。

---

## 🚀 快速开始

### 编译

```bash
go build -o nanobot-go ./cmd/nanobot-go
```

### 配置

运行 onboard 命令初始化配置：

```bash
./nanobot-go onboard
```

这将创建 `~/.nanobot-go/config.json`。编辑该文件以配置你的 Provider：

### 方式一：使用 OpenRouter（推荐）

```json
{
  "agents": {
    "defaults": {
      "model": "openrouter/anthropic/claude-3.5-sonnet",
      "maxTokens": 8192,
      "temperature": 0.1,
      "memoryWindow": 100
    }
  },
  "providers": {
    "openrouter": {
      "apiKey": "sk-or-v1-your-key-here",
      "apiBase": "https://openrouter.ai/api/v1"
    }
  }
}
```

获取 API Key: https://openrouter.ai/keys

### 方式二：使用本地代理

```json
{
  "agents": {
    "defaults": {
      "model": "custom/gpt-4",
      "maxTokens": 8192,
      "temperature": 0.1,
      "memoryWindow": 100
    }
  },
  "providers": {
    "custom": {
      "apiKey": "",
      "apiBase": "http://localhost:4141/v1"
    }
  }
}
```

### Model 格式说明

格式：`provider/model-name`

**OpenRouter 示例**：
- `openrouter/anthropic/claude-3.5-sonnet`
- `openrouter/openai/gpt-4o`
- `openrouter/google/gemini-pro-1.5`

**本地代理示例**：
- `custom/gpt-4` （provider 名称为 "custom"）
- `custom/llama-3` （使用本地模型）

### Workspace 结构

初始化后的 workspace 目录结构：

```
workspace/
├── memory/
│   ├── MEMORY.md          # 长期记忆（自动加载到系统提示词）
│   └── HISTORY.md         # 历史日志（可通过 grep 搜索）
├── sessions/              # JSONL 格式的 session 持久化
├── skills/                # 自定义 skills
├── AGENTS.md              # Agent 指南（从模板复制）
├── SOUL.md                # 人格定义
├── USER.md                # 用户配置
└── TOOLS.md               # 工具说明
```

### 交互模式（默认，无需参数）

```bash
# 运行交互式聊天
./nanobot-go
```

CLI 将进入交互式聊天模式。输入你的消息并按 **Enter** 发送。

| 命令 | 作用 |
|------|------|
| `/new` | 开始新的对话会话 |
| `/exit` 或 `/quit` | 退出 |
| `Ctrl-C` | 退出 |

### 单条消息模式（`-m`）

```bash
./nanobot-go agent -m "列出当前目录下的 Go 文件"
```

将助手回复打印到标准输出并退出。适合用于脚本。

### Gateway 模式

运行多通道服务器，同时接受来自 CLI 和 Feishu/Lark 的消息。

```bash
./nanobot-go gateway
```

Feishu 凭证可通过环境变量或配置文件设置：

```bash
export FEISHU_APP_ID=your_app_id
export FEISHU_APP_SECRET=your_secret
export FEISHU_ENCRYPT_KEY=your_key

./nanobot-go gateway
```

或在 `~/.nanobot-go/config.json` 中配置：

```json
{
  "channels": {
    "feishu": {
      "appId": "your_app_id",
      "appSecret": "your_secret",
      "encryptKey": "your_key",
      "allowFrom": ["ou_xxxx", "ou_yyyy"]
    }
  }
}
```

---

## 🎯 Skills 系统

### Skills 加载

Skills 是扩展 Agent 能力的 Markdown 文件（`SKILL.md`）。系统支持：

- **渐进式加载**：`always: true` 的 skills 全量加载到系统提示词，其他只加载摘要
- **依赖检查**：自动检查 `bins`（CLI 工具）和 `env`（环境变量）依赖
- **优先级**：workspace skills > 内置 skills

### Skill 格式

```markdown
---
name: memory
description: 两层记忆系统，支持 grep 搜索
always: true
requires:
  bins: [git, grep]
  env: [API_KEY]
---

# Memory

## 结构
- memory/MEMORY.md — 长期记忆
- memory/HISTORY.md — 历史日志
...
```

### 内置 Skills

- **memory** - 两层记忆系统使用指南
- **cron** - 定时任务和提醒功能

---

## 🧠 Memory 系统

### 两层记忆架构

1. **MEMORY.md** - 长期记忆
   - 用户偏好、项目上下文、重要关系
   - 自动加载到系统提示词
   - Agent 可通过 `write_file` / `edit_file` 工具更新

2. **HISTORY.md** - 历史日志
   - 仅追加的事件日志
   - 不加载到提示词，通过 `grep` 搜索
   - 由 consolidation 自动追加

### 自动 Consolidation

当对话历史超过 `memoryWindow`（默认 100 条）时：

1. 提取旧消息中的关键信息
2. 更新 MEMORY.md（长期记忆）
3. 追加摘要到 HISTORY.md
4. 清理 session，保留最近的消息

---

## 📋 完整命令行参数

### Agent / 单条消息模式

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `-model` | 配置文件中的值 | LLM 模型名称（格式：provider/model-name） |
| `-api-key` | 配置文件中的值 | API 密钥 |
| `-api-base` | 配置文件中的值 | API Base URL |
| `-workspace` | 配置文件中的值 | Workspace 目录（工具沙箱根目录） |
| `-max-iter` | `40` | 每条消息的最大 Agent 迭代次数 |
| `-temp` | `0.1` | 采样温度 |
| `-max-tokens` | `8192` | 最大响应 tokens |
| `-m` | — | 单条消息（非交互模式） |
| `-config` | `~/.nanobot-go/config.json` | 配置文件路径 |

### Gateway 子命令

包含以上所有参数，以及：

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `-port` | `18790` | Gateway 端口 |
| `-feishu-app-id` | — | Feishu App ID |
| `-feishu-app-secret` | — | Feishu App Secret |
| `-feishu-encrypt-key` | — | Feishu Encrypt Key |
| `-feishu-allow` | _(所有)_ | 允许的发送者 open IDs（可重复指定） |

---

## 🔧 使用特定的 Provider

```go
// OpenRouter（通过 "sk-or-" 密钥前缀自动检测）
p, _ := provider.NewProvider(provider.ProviderConfig{
    APIKey: "sk-or-v1-...",
    Model:  "anthropic/claude-opus-4-6",
})

// DeepSeek（通过 model 关键字自动检测）
p, _ := provider.NewProvider(provider.ProviderConfig{
    APIKey: "sk-...",
    Model:  "deepseek-chat",
})

// 自定义 / 本地服务器
p, _ := provider.NewProvider(provider.ProviderConfig{
    ProviderName: "custom",
    APIBase:      "http://localhost:8000/v1",
    APIKey:       "no-key",
    Model:        "my-local-model",
})

// 额外请求头（例如 OpenRouter site 跟踪）
p, _ := provider.NewProvider(provider.ProviderConfig{
    APIKey: "sk-or-...",
    Model:  "openai/gpt-4o",
    ExtraHeaders: map[string]string{
        "HTTP-Referer": "https://myapp.example",
        "X-Title":      "My App",
    },
})
```

---

## 🧪 运行测试

```bash
go test ./...
```

所有测试都使用 `httptest.NewServer` 模拟 HTTP 端点 — 不会发起真实的 API 调用。

---

## 🔄 与 Python nanobot 的对应关系

| Python | Go |
|--------|----|
| `providers/base.py` | `provider/interface.go` |
| `providers/registry.py` | `provider/registry.go` |
| `providers/custom_provider.py` | `provider/openai_compat.go` |
| `providers/litellm_provider.py` | `provider/factory.go` + `openai_compat.go` |
| `bus/events.py` + `queue.py` | `bus/bus.go` |
| `agent/tools/base.py` + `registry.py` | `tool/tool.go` |
| `agent/tools/shell.py` | `tool/shell.go` |
| `agent/tools/filesystem.py` | `tool/filesystem.go` |
| `agent/loop.py` | `agent/loop.go` + `context.go` + `session.go` |
| `agent/memory.py` | `agent/memory.go` |
| `agent/skills.py` | `agent/skills.go` |
| `agent/context.py` | `agent/prompt.go` + `agent/init.go` |
| `channels/base.py` | `channel/channel.go` |
| `channels/cli.py` | `channel/cli.go` |
| `channels/feishu.py` | `channel/feishu.go` |
| `cli/commands.py` (agent cmd) | `cmd/nanobot-go/main.go` + `gateway.go` |

---

## 📄 许可证

MIT License

---

## 🙏 致谢

本项目是 [NanoBot](https://github.com/libo/nanobot) Python 版本的 Go 语言移植版本。
