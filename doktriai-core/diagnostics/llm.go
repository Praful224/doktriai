package diagnostics

import (
	"context"
	"fmt"

	"google.golang.org/genai"
)

// DiagnoseLog queries the Gemini API using the official Go GenAI SDK
// to analyze container crash logs and explain the root cause in plain English.
func DiagnoseLog(ctx context.Context, logs string) (string, error) {
	// Client automatically reads GEMINI_API_KEY env variable
	client, err := genai.NewClient(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("failed to initialize GenAI client: %w", err)
	}

	prompt := fmt.Sprintf(
		"You are a Senior DevOps SRE. Analyze the following container crash logs. "+
			"Explain the root cause in plain English in less than 3 paragraphs, and provide "+
			"clear recommended steps to fix the issue:\n\n%s", logs,
	)

	resp, err := client.Models.GenerateContent(ctx, "gemini-2.5-flash", genai.Text(prompt), nil)
	if err != nil {
		return "", fmt.Errorf("failed to generate content: %w", err)
	}

	return resp.Text(), nil
}
