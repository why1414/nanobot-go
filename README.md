# nanobot-go / provider

Go implementation of NanoBot's LLM provider layer.

This module is the Go equivalent of the Python `nanobot/providers/` package.
It provides a uniform `LLMProvider` interface backed by direct HTTP calls to
any OpenAI-compatible `/chat/completions` endpoint — no SDK dependency, no
LiteLLM routing layer.

## Package layout

| File | Python equivalent | Purpose |
|------|-------------------|---------|
| `provider/interface.go` | `providers/base.py` | Core types and `LLMProvider` interface |
| `provider/registry.go` | `providers/registry.py` | `ProviderSpec` registry + lookup helpers |
| `provider/openai_compat.go` | `providers/custom_provider.py` | Direct HTTP OpenAI-compatible provider |
| `provider/factory.go` | config init logic | `NewProvider` factory wiring config → provider |

## Quick start

```go
import (
    "context"
    "fmt"
    "github.com/libo/nanobot-go/provider"
)

func main() {
    p, err := provider.NewProvider(provider.ProviderConfig{
        APIKey: "sk-...",
        Model:  "gpt-4o",
    })
    if err != nil {
        panic(err)
    }

    resp, err := p.Chat(context.Background(), []provider.Message{
        {Role: "user", Content: "Hello!"},
    }, provider.ChatOptions{})
    if err != nil {
        panic(err)
    }

    if resp.Content != nil {
        fmt.Println(*resp.Content)
    }
}
```

## Using a specific provider

```go
// OpenRouter (auto-detected by "sk-or-" key prefix)
p, _ := provider.NewProvider(provider.ProviderConfig{
    APIKey: "sk-or-v1-...",
    Model:  "anthropic/claude-opus-4-5",
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

## Tool calls

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

## Running tests

```bash
cd /path/to/nanobot-go
go test ./provider/...
```

All tests use `httptest.NewServer` to mock the HTTP endpoint — no real API
calls are made.

## Correspondence with Python

| Python | Go |
|--------|----|
| `LLMProvider` ABC | `LLMProvider` interface |
| `LLMResponse` dataclass | `LLMResponse` struct |
| `ToolCallRequest` dataclass | `ToolCallRequest` struct |
| `CustomProvider` | `OpenAICompatProvider` |
| `LiteLLMProvider` | factory + `OpenAICompatProvider` (direct HTTP) |
| `ProviderSpec` dataclass | `ProviderSpec` struct |
| `find_by_model()` | `FindByModel()` |
| `find_gateway()` | `FindGateway()` |
| `find_by_name()` | `FindByName()` |
