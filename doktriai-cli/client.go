package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// ANSI colour codes for terminal output
const (
	colReset  = "\033[0m"
	colGreen  = "\033[32m"
	colRed    = "\033[31m"
	colYellow = "\033[33m"
	colCyan   = "\033[36m"
	colGray   = "\033[90m"
	colBold   = "\033[1m"
)

// Client sends authenticated HTTP requests to the doktriai-api.
type Client struct {
	BaseURL string
	Role    string
	Actor   string
	Token   string // optional HMAC token; overrides Role/Actor headers if set
	HTTP    *http.Client
}

func NewClient(url, role, actor string) *Client {
	return &Client{
		BaseURL: url,
		Role:    role,
		Actor:   actor,
		HTTP:    &http.Client{},
	}
}

// WithToken returns a copy of the client using a signed HMAC token.
func (c *Client) WithToken(token string) *Client {
	cp := *c
	cp.Token = token
	return &cp
}

// Call makes an HTTP request and prints formatted JSON output.
func (c *Client) Call(method, path string, payload any) error {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, c.BaseURL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.Token != "" {
		req.Header.Set("X-Doktri-Token", c.Token)
	} else {
		req.Header.Set("X-Doktri-Role", c.Role)
		req.Header.Set("X-Doktri-Actor", c.Actor)
	}

	res, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}
	defer res.Body.Close()

	data, _ := io.ReadAll(res.Body)

	// Colour the status line
	statusColour := colGreen
	if res.StatusCode >= 400 {
		statusColour = colRed
	} else if res.StatusCode >= 300 {
		statusColour = colYellow
	}
	fmt.Printf("%s%s %s %d%s\n", colBold, method, path, res.StatusCode, colReset)
	if res.StatusCode >= 300 {
		fmt.Printf("%sstatus: %s%d%s\n", statusColour, colBold, res.StatusCode, colReset)
	}

	if res.StatusCode >= 400 {
		return fmt.Errorf("%s", strings.TrimSpace(string(data)))
	}

	prettyPrint(data)
	return nil
}

// CallMCP sends a JSON-RPC 2.0 request to the MCP endpoint.
func (c *Client) CallMCP(method string, params any) error {
	payload := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
	}
	if params != nil {
		payload["params"] = params
	}
	return c.Call("POST", "/api/mcp", payload)
}

// prettyPrint outputs JSON with coloured keys for terminal readability.
func prettyPrint(data []byte) {
	// Try to pretty-print as JSON
	var buf bytes.Buffer
	if err := json.Indent(&buf, data, "", "  "); err == nil {
		// Colourize: keys in cyan, strings in green, numbers in yellow
		lines := strings.Split(buf.String(), "\n")
		for _, line := range lines {
			line = colourJSONLine(line)
			fmt.Println(line)
		}
	} else {
		fmt.Println(string(data))
	}
}

// colourJSONLine applies basic ANSI colouring to a JSON line.
func colourJSONLine(line string) string {
	trimmed := strings.TrimSpace(line)
	if trimmed == "{" || trimmed == "}" || trimmed == "[" || trimmed == "]" ||
		strings.HasSuffix(trimmed, "{") || strings.HasSuffix(trimmed, "[") ||
		trimmed == "}," || trimmed == "]," {
		return colGray + line + colReset
	}
	// Key-value line
	if idx := strings.Index(line, ":"); idx > 0 {
		key := line[:idx+1]
		val := line[idx+1:]
		// Colour the value based on content
		valTrimmed := strings.TrimSpace(val)
		switch {
		case strings.HasPrefix(valTrimmed, "\"true\"") || valTrimmed == "true" || strings.HasPrefix(valTrimmed, "true"):
			val = strings.Replace(val, "true", colGreen+"true"+colReset, 1)
		case strings.HasPrefix(valTrimmed, "\"false\"") || valTrimmed == "false" || strings.HasPrefix(valTrimmed, "false"):
			val = strings.Replace(val, "false", colRed+"false"+colReset, 1)
		case strings.HasPrefix(valTrimmed, "\"error") || strings.HasPrefix(valTrimmed, "\"fail"):
			val = colRed + val + colReset
		case strings.HasPrefix(valTrimmed, "\"ok") || strings.HasPrefix(valTrimmed, "\"accept") || strings.HasPrefix(valTrimmed, "\"approved"):
			val = colGreen + val + colReset
		case strings.HasPrefix(valTrimmed, "\"warn") || strings.HasPrefix(valTrimmed, "\"pending"):
			val = colYellow + val + colReset
		case strings.HasPrefix(valTrimmed, "\""):
			val = colGreen + val + colReset
		default:
			// Numbers
			val = colYellow + val + colReset
		}
		return colCyan + key + colReset + val
	}
	return line
}
