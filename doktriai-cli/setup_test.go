package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSetupMCP_Claude(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("DOKTRIAI_TEST_HOME", tempDir)
	t.Setenv("APPDATA", filepath.Join(tempDir, "AppData", "Roaming")) // For Windows compatibility

	// Run setup first time (file doesn't exist)
	err := SetupMCP("claude")
	if err != nil {
		t.Fatalf("SetupMCP failed: %v", err)
	}

	// Determine expected path
	var expectedPath string
	if os.Getenv("APPDATA") != "" {
		expectedPath = filepath.Join(os.Getenv("APPDATA"), "Claude", "claude_desktop_config.json")
	} else {
		expectedPath = filepath.Join(tempDir, "Library", "Application Support", "Claude", "claude_desktop_config.json")
	}

	// Verify file was created
	body, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("failed to read created config: %v", err)
	}

	// Verify JSON structure
	var config map[string]any
	if err := json.Unmarshal(body, &config); err != nil {
		t.Fatalf("failed to parse config JSON: %v", err)
	}

	mcpServers, ok := config["mcpServers"].(map[string]any)
	if !ok {
		t.Fatal("config missing mcpServers block")
	}

	doktri, ok := mcpServers["doktriai"].(map[string]any)
	if !ok {
		t.Fatal("mcpServers missing doktriai block")
	}

	if doktri["command"] == "" {
		t.Error("expected command to be non-empty path")
	}

	env, ok := doktri["env"].(map[string]any)
	if !ok {
		t.Fatal("doktriai block missing env block")
	}

	if env["DOKTRIAI_TOKEN"] != "$DOKTRIAI_TOKEN" {
		t.Errorf("expected DOKTRIAI_TOKEN env reference, got %v", env["DOKTRIAI_TOKEN"])
	}

	// Run again with pre-existing settings (must merge)
	config["other_setting"] = "hello"
	newBody, _ := json.Marshal(config)
	_ = os.WriteFile(expectedPath, newBody, 0644)

	err = SetupMCP("claude")
	if err != nil {
		t.Fatalf("second SetupMCP failed: %v", err)
	}

	body, _ = os.ReadFile(expectedPath)
	var merged map[string]any
	json.Unmarshal(body, &merged)

	if merged["other_setting"] != "hello" {
		t.Error("expected setup to merge and preserve existing settings")
	}
}

func TestSetupMCP_Cursor(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("DOKTRIAI_TEST_HOME", tempDir)

	err := SetupMCP("cursor")
	if err != nil {
		t.Fatalf("SetupMCP failed: %v", err)
	}

	expectedPath := filepath.Join(tempDir, ".cursor", "mcp.json")
	body, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("failed to read created cursor config: %v", err)
	}

	var config map[string]any
	json.Unmarshal(body, &config)

	mcpServers, ok := config["mcpServers"].(map[string]any)
	if !ok {
		t.Fatal("cursor config missing mcpServers block")
	}

	if mcpServers["doktriai"] == nil {
		t.Error("cursor mcpServers missing doktriai block")
	}
}

func TestSetupMCP_Invalid(t *testing.T) {
	err := SetupMCP("invalid-agent")
	if err == nil {
		t.Error("expected error for invalid agent")
	}
}
