package main

import (
	"context"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/libo/nanobot-go/agent"
	"github.com/libo/nanobot-go/bus"
	"github.com/libo/nanobot-go/config"
	"github.com/libo/nanobot-go/cron"
	"github.com/libo/nanobot-go/provider"
	"github.com/libo/nanobot-go/tool"
)

// App holds all initialized components for a nanobot instance.
type App struct {
	Config       *config.Config
	Provider     provider.LLMProvider
	Tools        *tool.ToolRegistry
	CronService  *cron.CronService
	SkillsLoader *agent.SkillsLoader
	MemoryStore  *agent.MemoryStore
	MessageBus   *bus.MessageBus
	AgentLoop    *agent.AgentLoop
	WorkspaceDir string
}

// Initialize creates a fully initialized App instance from config.
func Initialize(cfgPath string) (*App, error) {
	// Load config
	cfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		return nil, err
	}

	// Determine workspace
	workspaceDir := cfg.WorkspacePath()

	// Determine model and provider
	model := cfg.GetModel()
	apiKey := cfg.GetAPIKey(model)
	apiBase := cfg.GetAPIBase(model)

	slog.Info("model configuration", "model", model)

	// Create provider
	p := provider.NewOpenAICompatProvider(apiKey, apiBase, model)

	// Initialize tools
	tools := tool.NewToolRegistry()
	tools.Register(tool.NewShellTool(workspaceDir, time.Duration(cfg.Tools.Exec.Timeout)*time.Second))
	tools.Register(tool.NewReadFileTool(workspaceDir))
	tools.Register(tool.NewWriteFileTool(workspaceDir))
	tools.Register(tool.NewEditFileTool(workspaceDir))
	tools.Register(tool.NewListDirTool(workspaceDir))

	// Initialize cron service (without callback for CLI mode)
	cronService := cron.NewCronService(filepath.Join(workspaceDir, "cron.json"), nil)
	tools.Register(tool.NewCronTool(cronService))

	// Initialize workspace (ensure directories and files exist)
	builtinSkillsDir := "" // Will use embedded skills if available
	if err := agent.EnsureWorkspace(workspaceDir, builtinSkillsDir); err != nil {
		slog.Warn("failed to initialize workspace", "error", err)
	}

	// Initialize skills loader
	skillsLoader := agent.NewSkillsLoader(workspaceDir, builtinSkillsDir)

	// Initialize memory store
	memoryStore := agent.NewMemoryStore(workspaceDir)

	// Create message bus
	mb := bus.NewMessageBus(32)

	// Create agent loop
	agentLoop := agent.NewAgentLoop(mb, p, tools, agent.AgentOptions{
		Model:        model,
		MaxIter:      cfg.Agents.Defaults.MaxToolIterations,
		Temperature:  cfg.Agents.Defaults.Temperature,
		MaxTokens:    cfg.Agents.Defaults.MaxTokens,
		MemoryWindow: cfg.Agents.Defaults.MemoryWindow,
	}, memoryStore, skillsLoader, workspaceDir)

	return &App{
		Config:       cfg,
		Provider:     p,
		Tools:        tools,
		CronService:  cronService,
		SkillsLoader: skillsLoader,
		MemoryStore:  memoryStore,
		MessageBus:   mb,
		AgentLoop:    agentLoop,
		WorkspaceDir: workspaceDir,
	}, nil
}

// StartCron starts the cron service if available.
func (a *App) StartCron(ctx context.Context) error {
	if a.CronService == nil {
		return nil
	}
	return a.CronService.Start(ctx)
}

// StopCron stops the cron service.
func (a *App) StopCron() {
	if a.CronService != nil {
		a.CronService.Stop()
	}
}
