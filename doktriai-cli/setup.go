package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// SetupMCP configures Claude Desktop or Cursor to connect to the DoktriAI MCP server.
func SetupMCP(agent string) error {
	agent = strings.ToLower(strings.TrimSpace(agent))
	if agent != "claude" && agent != "cursor" {
		return fmt.Errorf("unsupported agent %q; must be 'claude' or 'cursor'", agent)
	}

	executablePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	var configPath string
	var configName string

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get user home directory: %w", err)
	}
	if testHome := os.Getenv("DOKTRIAI_TEST_HOME"); testHome != "" {
		homeDir = testHome
	}

	switch agent {
	case "claude":
		configName = "Claude Desktop"
		switch runtime.GOOS {
		case "windows":
			appData := os.Getenv("APPDATA")
			if appData == "" {
				appData = filepath.Join(homeDir, "AppData", "Roaming")
			}
			configPath = filepath.Join(appData, "Claude", "claude_desktop_config.json")
		case "darwin":
			configPath = filepath.Join(homeDir, "Library", "Application Support", "Claude", "claude_desktop_config.json")
		default:
			// Linux/Other fallback
			configPath = filepath.Join(homeDir, ".config", "Claude", "claude_desktop_config.json")
		}
	case "cursor":
		configName = "Cursor"
		configPath = filepath.Join(homeDir, ".cursor", "mcp.json")
	}

	fmt.Printf("Configuring %s MCP connection...\n", configName)
	fmt.Printf("Config file path: %s\n", configPath)

	// Create parent directories if they don't exist
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return fmt.Errorf("failed to create configuration directories: %w", err)
	}

	// Read existing configuration if it exists
	var config map[string]any
	body, err := os.ReadFile(configPath)
	if err == nil && len(body) > 0 {
		if err := json.Unmarshal(body, &config); err != nil {
			fmt.Printf("%sWarning: existing config file was invalid JSON, resetting...%s\n", colYellow, colReset)
			config = make(map[string]any)
		}
	} else {
		config = make(map[string]any)
	}

	// Get or initialize mcpServers map
	mcpServersRaw, exists := config["mcpServers"]
	var mcpServers map[string]any
	if exists {
		mcpServers, _ = mcpServersRaw.(map[string]any)
	}
	if mcpServers == nil {
		mcpServers = make(map[string]any)
	}

	// Define our mcp server block
	doktriServer := map[string]any{
		"command": executablePath,
		"args":    []string{"mcp"},
		"env": map[string]string{
			"DOKTRIAI_TOKEN": "$DOKTRIAI_TOKEN",
		},
	}

	// Update block
	mcpServers["doktriai"] = doktriServer
	config["mcpServers"] = mcpServers

	// Write back to config file
	newBody, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize updated configuration: %w", err)
	}

	if err := os.WriteFile(configPath, newBody, 0644); err != nil {
		return fmt.Errorf("failed to write configuration file: %w", err)
	}

	fmt.Printf("%sSuccess! Configured %s to run DoktriAI MCP server.%s\n\n", colGreen, configName, colReset)
	printOnboardingInstructions(agent)

	return nil
}

func printOnboardingInstructions(agent string) {
	fmt.Println(colBold + "Step-by-Step Integration Guide:" + colReset)
	fmt.Println("1. Obtain a valid JWT or HMAC token for the AI agent (e.g. using `doktriai-cli gen-token` command).")
	fmt.Println("2. Set the `DOKTRIAI_TOKEN` environment variable in your environment:")
	if runtime.GOOS == "windows" {
		fmt.Printf("   %sWindows Command Prompt:%s\n", colCyan, colReset)
		fmt.Println("     setx DOKTRIAI_TOKEN \"your_issued_token_value\"")
		fmt.Printf("   %sWindows PowerShell:%s\n", colCyan, colReset)
		fmt.Println("     [System.Environment]::SetEnvironmentVariable('DOKTRIAI_TOKEN', 'your_issued_token_value', 'User')")
	} else {
		fmt.Printf("   %smacOS / Linux (Bash/Zsh):%s\n", colCyan, colReset)
		fmt.Println("     export DOKTRIAI_TOKEN=\"your_issued_token_value\"")
		fmt.Println("     (Add this line to your ~/.zshrc or ~/.bashrc to make it persistent)")
	}
	fmt.Printf("3. %sRestart %s%s to load the new config and environment variables.\n", colBold, getFriendlyAgentName(agent), colReset)
	fmt.Println("4. Ask the AI agent: \"list all workloads\" or \"deploy nginx:alpine named test-app\" to test.")
}

func getFriendlyAgentName(agent string) string {
	if agent == "cursor" {
		return "Cursor IDE"
	}
	return "Claude Desktop"
}
