# nanobot-go

A full Go port of [NanoBot](https://github.com/libo/nanobot) — a lightweight,
tool-calling AI agent that works with any OpenAI-compatible LLM endpoint.

No SDK dependency for the LLM layer; direct `net/http` calls only.
External dependencies are limited to the TUI renderer
([Bubble Tea](https://github.com/charmbracelet/bubbletea) /
[Lipgloss](https://github.com/charmbracelet/lipgloss)) and the Feishu
[lark-oapi](https://github.com/larksuite/oapi-sdk-go) SDK.

---

## Features

| Status | Feature |
|--------|---------|
| ✅ | OpenAI-compatible provider (direct HTTP, no SDK) |
| ✅ | Tool system — shell exec, read/write/edit/list file tools |
| ✅ | ReAct agent loop with multi-turn session history |
| ✅ | CLI channel (stdin/stdout) |
| ✅ | TUI mode (Bubble Tea interactive terminal UI) |
| ✅ | Gateway subcommand (multi-channel server) |
| ✅ | Feishu/Lark channel (WebSocket long connection, no public IP needed) |
| ☐ | Telegram channel |
| ☐ | Config file support (YAML/TOML) |
| ☐ | Memory / session persistence (disk) |
| ☐ | Docker support |
| ☐ | Cron / heartbeat service |
| ☐ | Multi-agent orchestration |
| ☐ | Web search tool |

---

## Package layout

| Package | File(s) | Role |
|---------|---------|------|
| `provider` | `interface.go` | Core types: `Message`, `LLMResponse`, `LLMProvider` interface |
| `provider` | `registry.go` | `ProviderSpec` registry + `FindByModel` / `FindGateway` / `FindByName` |
| `provider` | `openai_compat.go` | Direct HTTP OpenAI-compatible provider |
| `provider` | `factory.go` | `NewProvider(ProviderConfig)` factory |
| `bus` | `bus.go` | `MessageBus`: buffered Go channels for inbound / outbound messages |
| `tool` | `tool.go` | `Tool` interface + `ToolRegistry` (Register / Get / Execute) |
| `tool` | `shell.go` | `ShellTool` ("exec"): runs `sh -c` with timeout, captures stdout+stderr |
| `tool` | `filesystem.go` | `ReadFileTool`, `WriteFileTool`, `EditFileTool`, `ListDirTool` |
| `agent` | `session.go` | `SessionMessage`, `Session`, `SessionManager` (in-memory) |
| `agent` | `context.go` | `BuildMessages`, `AddAssistantMessage`, `AddToolResult` helpers |
| `agent` | `loop.go` | `AgentLoop`: `Run(ctx)`, `ProcessMessage`, `runReActLoop` |
| `channel` | `channel.go` | `Channel` interface + `BaseChannel` (allowlist, `HandleMessage`) |
| `channel` | `cli.go` | `CLIChannel`: stdin loop, spinner, `/new` `/exit` `/quit` commands |
| `channel` | `tui.go` | `TUIChannel`: Bubble Tea interactive TUI |
| `channel` | `feishu.go` | `FeishuChannel`: Feishu/Lark bot via WebSocket long connection |
| `cmd/nanobot` | `main.go` | Entry point: flags, wires provider + bus + agent + channel |
| `cmd/nanobot` | `gateway.go` | `gateway` subcommand: multi-channel server (CLI + Feishu) |

---

## Architecture

```
┌──────────────┐     InboundMessage     ┌─────────────┐
│   Channel    │ ─────────────────────► │ MessageBus  │
│ (CLI / TUI / │                        │  (buffered  │
│  Feishu/...) │ ◄───────────────────── │   channels) │
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

Channels publish user messages onto the bus as `InboundMessage` values.
The `AgentLoop` consumes them, runs a ReAct loop (LLM → tool calls →
observations → LLM …), then publishes the final reply as an
`OutboundMessage`.  An outbound dispatcher goroutine in `main` (or
`gateway`) reads outbound messages and routes each one back to the
originating channel by name.

---

## Quick start

### Build

```bash
go build -o nanobot ./cmd/nanobot
```

### TUI mode (default — no flags)

```bash
# Uses Copilot proxy on localhost:4141 by default
./nanobot

# Anthropic
./nanobot -api-key sk-ant-xxx -model claude-sonnet-4-6

# OpenRouter
./nanobot -api-key sk-or-xxx \
          -api-base https://openrouter.ai/api/v1 \
          -model anthropic/claude-opus-4-6
```

The TUI renders a full-screen chat interface. Type your message and press
**Enter**. Commands:

| Command | Action |
|---------|--------|
| `/new` | Start a new conversation session |
| `/exit` or `/quit` | Quit |
| `Ctrl-C` | Quit |

### Single message (`-m`)

```bash
./nanobot -m "List the Go files in the current directory"
```

Prints the assistant reply to stdout and exits. Useful for scripting.

### Gateway mode

Runs a multi-channel server that accepts messages from the CLI and
optionally from Feishu/Lark simultaneously.

```bash
./nanobot gateway \
  -port 18790 \
  -feishu-app-id    $FEISHU_APP_ID \
  -feishu-app-secret $FEISHU_APP_SECRET \
  -feishu-encrypt-key $FEISHU_ENCRYPT_KEY \
  -feishu-allow ou_xxxx \
  -feishu-allow ou_yyyy
```

Feishu credentials can also be supplied via environment variables
`FEISHU_APP_ID`, `FEISHU_APP_SECRET`, `FEISHU_ENCRYPT_KEY`.

If no Feishu credentials are provided the gateway starts with the CLI
channel only.

---

## All flags

### Agent / TUI / single-message mode

| Flag | Default | Description |
|------|---------|-------------|
| `-model` | `claude-haiku-4.5` | LLM model name |
| `-api-key` | _(env)_ | API key (falls back to `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, …) |
| `-api-base` | `http://localhost:4141/v1` | API base URL |
| `-workspace` | cwd | Workspace directory (tool sandbox root) |
| `-max-iter` | `40` | Max agent iterations per message |
| `-temp` | `0.1` | Sampling temperature |
| `-max-tokens` | `65536` | Max response tokens |
| `-m` | — | Single message (non-interactive) |

### Gateway subcommand

All of the above flags, plus:

| Flag | Default | Description |
|------|---------|-------------|
| `-port` | `18790` | Gateway port |
| `-feishu-app-id` | — | Feishu App ID |
| `-feishu-app-secret` | — | Feishu App Secret |
| `-feishu-encrypt-key` | — | Feishu Encrypt Key |
| `-feishu-allow` | _(all)_ | Allowed sender open IDs (repeatable) |

---

## Using a specific provider

```go
// OpenRouter (auto-detected by "sk-or-" key prefix)
p, _ := provider.NewProvider(provider.ProviderConfig{
    APIKey: "sk-or-v1-...",
    Model:  "anthropic/claude-opus-4-6",
})

// DeepSeek (auto-detected by model keyword)
p, _ := provider.NewProvider(provider.ProviderConfig{
    APIKey: "sk-...",
    Model:  "deepseek-chat",
})

// Custom / local server
p, _ := provider.NewProvider(provider.ProviderConfig{
    ProviderName: "custom",
    APIBase:      "http://localhost:8000/v1",
    APIKey:       "no-key",
    Model:        "my-local-model",
})

// Extra headers (e.g. OpenRouter site tracking)
p, _ := provider.NewProvider(provider.ProviderConfig{
    APIKey: "sk-or-...",
    Model:  "openai/gpt-4o",
    ExtraHeaders: map[string]string{
        "HTTP-Referer": "https://myapp.example",
        "X-Title":      "My App",
    },
})
```

## Tool calls (provider API)

```go
tools := []provider.Tool{{
    Type: "function",
    Function: provider.ToolFunction{
        Name:        "get_weather",
        Description: "Return current weather",
        Parameters: map[string]any{
            "type": "object",
            "properties": map[string]any{
                "location": map[string]any{"type": "string"},
            },
            "required": []string{"location"},
        },
    },
}}

resp, err := p.Chat(ctx, messages, provider.ChatOptions{Tools: tools})
if resp.HasToolCalls() {
    for _, tc := range resp.ToolCalls {
        fmt.Printf("call %s(%v)\n", tc.Name, tc.Arguments)
    }
}
```

## Provider registry

```go
// Look up a provider by model name
spec := provider.FindByModel("deepseek-chat")  // → ProviderSpec{Name:"deepseek", ...}

// Detect a gateway from an API key
spec := provider.FindGateway("", "sk-or-v1-abc", "")  // → ProviderSpec{Name:"openrouter", ...}

// Direct name lookup
spec := provider.FindByName("dashscope")
```

---

## Running tests

```bash
go test ./...
```

All tests use `httptest.NewServer` to mock the HTTP endpoint — no real API
calls are made.

---

## Correspondence with Python nanobot

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
| `channels/base.py` | `channel/channel.go` |
| `channels/cli.py` | `channel/cli.go` |
| `channels/feishu.py` | `channel/feishu.go` |
| `cli/commands.py` (agent cmd) | `cmd/nanobot/main.go` + `gateway.go` |
