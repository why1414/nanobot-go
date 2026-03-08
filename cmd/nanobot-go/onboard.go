package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/why1414/nanobot-go/agent"
	"github.com/why1414/nanobot-go/config"
)

// runOnboard initializes nanobot-go configuration and workspace.
func runOnboard(args []string) {
	fs := flag.NewFlagSet("onboard", flag.ExitOnError)
	configPath := fs.String("config", "", "Path to config file")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: nanobot-go onboard [flags]\n\n")
		fmt.Fprintf(os.Stderr, "Initialize nanobot-go configuration and workspace.\n\nFlags:\n")
		fs.PrintDefaults()
	}
	_ = fs.Parse(args)

	// Determine config path
	cfgPath := *configPath
	if cfgPath == "" {
		cfgPath = config.GetConfigPath()
	}

	cfgDir := filepath.Dir(cfgPath)

	fmt.Println()
	fmt.Println("🤖 nanobot-go")
	fmt.Println()

	// Check if config already exists
	if _, err := os.Stat(cfgPath); err == nil {
		fmt.Printf("Config already exists at %s\n", cfgPath)
		fmt.Println("  y = overwrite with defaults (existing values will be lost)")
		fmt.Println("  N = refresh config, keeping existing values and adding new fields")

		fmt.Print("Overwrite? [y/N]: ")
		var response string
		fmt.Scanln(&response)

		if response == "y" || response == "Y" {
			// Create new default config
			cfg := config.DefaultConfig()
			if err := config.SaveConfig(cfg, cfgPath); err != nil {
				fmt.Fprintf(os.Stderr, "Error saving config: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("✓ Config reset to defaults at %s\n", cfgPath)
		} else {
			// Load existing and re-save (to add new fields)
			cfg, err := config.LoadConfig(cfgPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
				os.Exit(1)
			}
			if err := config.SaveConfig(cfg, cfgPath); err != nil {
				fmt.Fprintf(os.Stderr, "Error saving config: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("✓ Config refreshed at %s (existing values preserved)\n", cfgPath)
		}
	} else {
		// Create new config
		cfg := config.DefaultConfig()
		if err := os.MkdirAll(cfgDir, 0755); err != nil {
			fmt.Fprintf(os.Stderr, "Error creating config directory: %v\n", err)
			os.Exit(1)
		}
		if err := config.SaveConfig(cfg, cfgPath); err != nil {
			fmt.Fprintf(os.Stderr, "Error saving config: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("✓ Created config at %s\n", cfgPath)
	}

	// Load config to get workspace path
	cfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	workspace := cfg.WorkspacePath()

	// Determine templates directory (relative to executable)
	execPath, _ := os.Executable()
	templatesDir := filepath.Join(filepath.Dir(execPath), "..", "templates")
	if _, err := os.Stat(templatesDir); os.IsNotExist(err) {
		// Try relative to current directory
		templatesDir = "templates"
		if _, err := os.Stat(templatesDir); os.IsNotExist(err) {
			templatesDir = "" // No templates available
		}
	}

	// Initialize workspace using agent.InitWorkspace
	if err := agent.EnsureWorkspace(workspace, templatesDir); err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing workspace: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✓ Initialized workspace at %s\n", workspace)

	// List created files
	if templatesDir != "" {
		fmt.Println("  Created memory/")
		fmt.Println("  Created memory/MEMORY.md")
		fmt.Println("  Created memory/HISTORY.md")
		fmt.Println("  Created sessions/")
		fmt.Println("  Created skills/")
		fmt.Println("  Created AGENTS.md (from template)")
		fmt.Println("  Created SOUL.md (from template)")
		fmt.Println("  Created USER.md (from template)")
		fmt.Println("  Created TOOLS.md (from template)")
	} else {
		fmt.Println("  Created memory/")
		fmt.Println("  Created memory/MEMORY.md")
		fmt.Println("  Created memory/HISTORY.md")
		fmt.Println("  Created sessions/")
		fmt.Println("  Created skills/")
	}

	fmt.Println()
	fmt.Println("🤖 nanobot-go is ready!")
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  1. Add your API key to ~/.nanobot-go/config.json")
	fmt.Println("     Get one at: https://openrouter.ai/keys")
	fmt.Println("  2. Chat: nanobot-go agent -m \"Hello!\"")
	fmt.Println()
}
