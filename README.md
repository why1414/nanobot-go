# nanobot-go

[English](README.en.md) | 中文

[NanoBot](https://github.com/HKUDS/nanobot) 的 Go 语言完整实现 — 一个轻量级的、支持工具调用的 AI Agent，兼容任何 OpenAI API 格式的 LLM 端点。

## ✨ 核心特性

- **OpenAI 兼容 Provider** — 直接 HTTP 调用，无需 SDK，支持 OpenRouter、本地代理等
- **长期记忆系统** — 两层记忆架构（MEMORY.md + HISTORY.md），自动 consolidation
- **Skills 扩展** — 渐进式加载，支持依赖检查和自定义能力
- **系统提示词模板** — 支持 AGENTS.md、SOUL.md、USER.md、TOOLS.md 等模板文件

---

## 📦 项目结构

- **`provider/`** — LLM Provider 实现（OpenAI 兼容）
- **`agent/`** — Agent 核心逻辑（ReAct 循环、记忆、Skills、会话管理）
- **`bus/`** — 消息总线（Channel 间通信）
- **`channel/`** — 通道接口（CLI、Feishu/Lark）
- **`tool/`** — 工具系统（Shell、文件系统操作）
- **`config/`** — 配置文件解析
- **`cron/`** — 定时任务服务
- **`skills/`** — 内置 Skills（memory、cron）
- **`templates/`** — 系统提示词模板文件
- **`cmd/nanobot-go/`** — 程序入口
  - `main.go` — 命令路由分发
  - `agent.go` — agent 子命令（交互式/单消息模式）
  - `gateway.go` — gateway 子命令（多通道服务）
  - `onboard.go` — onboard 子命令（初始化配置）
  - `app.go` — 共享初始化逻辑

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

## 📋 命令行参数

所有配置项（模型、API、Workspace、Feishu 凭证等）均在 `config.json` 中设置。

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `-config` | `~/.nanobot-go/config.json` | 配置文件路径 |
| `-m` | — | 单条消息模式（非交互） |

---

## 📄 许可证

MIT License

---

## 🙏 致谢

本项目是 [NanoBot](https://github.com/HKUDS/nanobot) Python 版本的 Go 语言移植版本。
