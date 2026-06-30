package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/praful224/doktriai/doktriai-packages"
)

type appsConnectionsOpenResponse struct {
	OK    bool   `json:"ok"`
	URL   string `json:"url"`
	Error string `json:"error"`
}

type socketModeEnvelope struct {
	Type      string          `json:"type"`
	EnvelopeID string         `json:"envelope_id"`
	Payload   json.RawMessage `json:"payload"`
}

type slackEventPayload struct {
	Event struct {
		Type    string `json:"type"`
		User    string `json:"user"`
		Text    string `json:"text"`
		Channel string `json:"channel"`
	} `json:"event"`
}

// StartSlack launches the Socket Mode background daemon to process ChatOps commands.
func (s *Server) StartSlack(ctx context.Context) {
	appToken := os.Getenv("SLACK_APP_TOKEN")
	botToken := os.Getenv("SLACK_BOT_TOKEN")

	if appToken == "" || botToken == "" {
		log.Println("Slack ChatOps disabled: SLACK_APP_TOKEN or SLACK_BOT_TOKEN environment variables not set")
		return
	}

	go s.slackSocketModeLoop(ctx, appToken, botToken)
}

func (s *Server) slackSocketModeLoop(ctx context.Context, appToken, botToken string) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			wsURL, err := s.getSlackWSURL(ctx, appToken)
			if err != nil {
				log.Printf("Slack connection URL fetch failed: %v. Retrying in 10s...", err)
				time.Sleep(10 * time.Second)
				continue
			}

			if err := s.runSlackWebSocket(ctx, wsURL, botToken); err != nil {
				log.Printf("Slack WebSocket error: %v. Reconnecting in 5s...", err)
				time.Sleep(5 * time.Second)
			}
		}
	}
}

func (s *Server) getSlackWSURL(ctx context.Context, appToken string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", "https://slack.com/api/apps.connections.open", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+appToken)
	req.Header.Set("Content-type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var openResp appsConnectionsOpenResponse
	if err := json.Unmarshal(body, &openResp); err != nil {
		return "", err
	}

	if !openResp.OK {
		return "", fmt.Errorf("Slack API error: %s", openResp.Error)
	}

	return openResp.URL, nil
}

func (s *Server) runSlackWebSocket(ctx context.Context, wsURL, botToken string) error {
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	conn, _, err := dialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	log.Println("Slack ChatOps Socket Mode client connected successfully")
	s.bus.Publish(packages.Event{Level: "ok", Source: "slack-chatops", Message: "Slack Socket Mode listener established"})

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return err
			}

			var envelope socketModeEnvelope
			if err := json.Unmarshal(msg, &envelope); err != nil {
				log.Printf("Failed to unmarshal Slack message: %v", err)
				continue
			}

			// Acknowledge all envelopes immediately to keep connection alive
			if envelope.EnvelopeID != "" {
				ack := map[string]any{"envelope_id": envelope.EnvelopeID}
				ackBytes, _ := json.Marshal(ack)
				_ = conn.WriteMessage(websocket.TextMessage, ackBytes)
			}

			if envelope.Type == "events_api" {
				var payload slackEventPayload
				if err := json.Unmarshal(envelope.Payload, &payload); err == nil {
					if payload.Event.Type == "message" && payload.Event.User != "" {
						go s.handleSlackMessage(ctx, payload.Event.Text, payload.Event.Channel, botToken)
					}
				}
			}
		}
	}
}

func (s *Server) handleSlackMessage(ctx context.Context, text, channel, botToken string) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "!doktri ") {
		return
	}

	parts := strings.Fields(text[8:])
	if len(parts) == 0 {
		s.sendSlackResponse(ctx, channel, "Usage: `!doktri <list|status|deploy|delete>`", botToken)
		return
	}

	cmd := strings.ToLower(parts[0])
	switch cmd {
	case "list":
		workloads := s.store.ListWorkloads()
		if len(workloads) == 0 {
			s.sendSlackResponse(ctx, channel, "No workloads currently defined.", botToken)
			return
		}
		var sb strings.Builder
		sb.WriteString("*Active Workloads:*\n")
		for _, w := range workloads {
			sb.WriteString(fmt.Sprintf("• *%s* (Image: `%s`, Replicas: %d)\n", w.Name, w.Image, w.Replicas))
		}
		s.sendSlackResponse(ctx, channel, sb.String(), botToken)

	case "status":
		if len(parts) < 2 {
			s.sendSlackResponse(ctx, channel, "Usage: `!doktri status <workload-name>`", botToken)
			return
		}
		name := parts[1]
		wl, ok := s.store.GetWorkload(name)
		if !ok {
			s.sendSlackResponse(ctx, channel, fmt.Sprintf("Workload %q not found.", name), botToken)
			return
		}
		s.sendSlackResponse(ctx, channel, fmt.Sprintf("*Workload %s Status:*\nImage: `%s`\nReplicas: %d\nRuntime: `%s`", wl.Name, wl.Image, wl.Replicas, wl.Runtime), botToken)

	case "deploy":
		if len(parts) < 3 {
			s.sendSlackResponse(ctx, channel, "Usage: `!doktri deploy <name> <image>`", botToken)
			return
		}
		name, image := parts[1], parts[2]
		spec := packages.WorkloadSpec{
			Name:     name,
			Image:    image,
			Replicas: 1,
		}
		if err := s.engine.Apply(ctx, "slack-chatops", spec); err != nil {
			s.sendSlackResponse(ctx, channel, fmt.Sprintf("Deployment failed: %v", err), botToken)
			return
		}
		s.sendSlackResponse(ctx, channel, fmt.Sprintf("Workload %q successfully queued for deployment.", name), botToken)

	case "delete":
		if len(parts) < 2 {
			s.sendSlackResponse(ctx, channel, "Usage: `!doktri delete <workload-name>`", botToken)
			return
		}
		name := parts[1]
		if err := s.engine.Delete(ctx, "slack-chatops", name); err != nil {
			s.sendSlackResponse(ctx, channel, fmt.Sprintf("Deletion failed: %v", err), botToken)
			return
		}
		s.sendSlackResponse(ctx, channel, fmt.Sprintf("Workload %q successfully deleted.", name), botToken)

	default:
		s.sendSlackResponse(ctx, channel, fmt.Sprintf("Unknown command %q. Allowed: `list`, `status`, `deploy`, `delete`", cmd), botToken)
	}
}

func (s *Server) sendSlackResponse(ctx context.Context, channel, text, botToken string) {
	payload := map[string]string{
		"channel": channel,
		"text":    text,
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "POST", "https://slack.com/api/chat.postMessage", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+botToken)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	resp, err := http.DefaultClient.Do(req)
	if err == nil {
		_ = resp.Body.Close()
	}
}
