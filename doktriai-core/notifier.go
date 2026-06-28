package core

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/praful224/doktriai/doktriai-packages"
)

// PlanEvent is the webhook payload fired on PTE plan lifecycle changes.
type PlanEvent struct {
	Event     string                `json:"event"` // plan_created|approved|rejected|expired
	PlanID    string                `json:"planId"`
	Workload  string                `json:"workload"`
	Actor     string                `json:"actor"`
	Reason    string                `json:"reason"`
	ExpiresAt *time.Time            `json:"expiresAt,omitempty"`
	Spec      packages.WorkloadSpec `json:"spec"`
	Timestamp time.Time             `json:"timestamp"`
}

// DriftEvent is the webhook payload fired when actual ≠ desired state.
type DriftEvent struct {
	Event           string    `json:"event"` // always "drift_detected"
	Workload        string    `json:"workload"`
	DesiredReplicas int       `json:"desiredReplicas"`
	ActualReplicas  int       `json:"actualReplicas"`
	Timestamp       time.Time `json:"timestamp"`
}

// SlackBlock is a minimal Slack Incoming Webhook payload for PTE notifications.
type SlackBlock struct {
	Text   string `json:"text"`
	Blocks []any  `json:"blocks,omitempty"`
}

// NotifyPTEWebhook sends a PTE plan event to the configured generic webhook.
func NotifyPTEWebhook(event PlanEvent) {
	url := GetPolicy().Notifications.PTEWebhookURL
	if url == "" {
		return
	}
	timeout := GetPolicy().Notifications.PTEWebhookTimeoutSec
	if timeout <= 0 {
		timeout = 5
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
		defer cancel()
		body, _ := json.Marshal(event)
		req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			log.Printf("notifier: PTE webhook failed: %v", err)
			return
		}
		defer resp.Body.Close()
	}()
}

// NotifySlack sends a formatted PTE notification to the configured Slack webhook.
func NotifySlack(plan *packages.PendingPlan, apiBaseURL string) {
	url := GetPolicy().Notifications.SlackWebhookURL
	if url == "" {
		return
	}
	if apiBaseURL == "" {
		apiBaseURL = "http://localhost:18080"
	}
	go func() {
		token, _ := GenerateRequestToken("slack-operator", "admin", "slack-notifier", "cluster:deploy", "Slack Action Button")

		proposedChange := fmt.Sprintf("Deploy image `%s` with replicas `%d`", plan.Spec.Image, plan.Spec.Replicas)
		if plan.Spec.Image == "" {
			proposedChange = "Delete workload"
		}

		riskLevel := "MEDIUM"
		if plan.Spec.Replicas > 5 {
			riskLevel = "HIGH (High replica count)"
		} else if plan.Spec.Image == "" {
			riskLevel = "HIGH (Deletion of workload)"
		} else {
			// Check sensitive env keys
			sensitiveKeys := []string{"SECRET", "KEY", "TOKEN", "PASSWORD", "PASSWD", "CREDENTIAL", "PRIVATE_KEY"}
			for k := range plan.Spec.Env {
				for _, sk := range sensitiveKeys {
					if strings.Contains(strings.ToUpper(k), sk) {
						riskLevel = "HIGH (Sensitive credentials in environment)"
						break
					}
				}
			}
		}

		expiresStr := fmt.Sprintf("%s (15-minute countdown)", plan.ExpiresAt.Format("15:04:05 MST"))

		emoji := "🔴"
		textHeader := fmt.Sprintf("%s *PTE Gate — Human Approval Required*", emoji)
		textDetails := fmt.Sprintf(
			"*Workload:* `%s`\n"+
				"*Requested by:* `%s`\n"+
				"*Proposed Change:* %s\n"+
				"*Risk Level:* `%s`\n"+
				"*Expires:* %s",
			plan.Spec.Name, plan.RequestedBy, proposedChange, riskLevel, expiresStr,
		)

		payload := SlackBlock{
			Text: "PTE Gate — Human Approval Required",
			Blocks: []any{
				map[string]any{
					"type": "section",
					"text": map[string]string{
						"type": "mrkdwn",
						"text": textHeader + "\n\n" + textDetails,
					},
				},
				map[string]any{
					"type": "actions",
					"elements": []any{
						map[string]any{
							"type": "button",
							"text": map[string]string{
								"type": "plain_text",
								"text": "✅ Approve",
							},
							"style": "primary",
							"url":   fmt.Sprintf("%s/api/plan/%s/approve?token=%s", apiBaseURL, plan.ID, token),
						},
						map[string]any{
							"type": "button",
							"text": map[string]string{
								"type": "plain_text",
								"text": "❌ Reject",
							},
							"style": "danger",
							"url":   fmt.Sprintf("%s/api/plan/%s/reject?token=%s", apiBaseURL, plan.ID, token),
						},
					},
				},
			},
		}

		body, _ := json.Marshal(payload)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, _ := http.DefaultClient.Do(req)
		if resp != nil {
			resp.Body.Close()
		}
	}()
}

// NotifyDrift sends a drift alert to the configured webhooks.
func NotifyDrift(workload string, desired, actual int) {
	driftURL := GetPolicy().Notifications.DriftWebhookURL
	if driftURL != "" {
		go func() {
			event := DriftEvent{
				Event:           "drift_detected",
				Workload:        workload,
				DesiredReplicas: desired,
				ActualReplicas:  actual,
				Timestamp:       time.Now().UTC(),
			}
			body, _ := json.Marshal(event)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, "POST", driftURL, bytes.NewReader(body))
			if err != nil {
				return
			}
			req.Header.Set("Content-Type", "application/json")
			resp, err := http.DefaultClient.Do(req)
			if err == nil && resp != nil {
				resp.Body.Close()
			}
		}()
	}

	slackURL := GetPolicy().Notifications.SlackWebhookURL
	if slackURL != "" {
		go func() {
			emoji := "⚠️"
			textHeader := fmt.Sprintf("%s *Workload Replica Drift Detected*", emoji)
			
			clusterCtx := os.Getenv("DOKTRIAI_CLUSTER_CONTEXT")
			if clusterCtx == "" {
				clusterCtx = "local-dev-cluster"
			}

			textDetails := fmt.Sprintf(
				"*Workload:* `%s`\n"+
					"*Expected Replicas:* `%d`\n"+
					"*Actual Replicas:* `%d`\n"+
					"*Cluster Context:* `%s`\n"+
					"*Status:* 🔴 Diverged from desired state",
				workload, desired, actual, clusterCtx,
			)

			payload := SlackBlock{
				Text: "Workload Replica Drift Detected",
				Blocks: []any{
					map[string]any{
						"type": "section",
						"text": map[string]string{
							"type": "mrkdwn",
							"text": textHeader + "\n\n" + textDetails,
						},
					},
				},
			}

			body, _ := json.Marshal(payload)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, "POST", slackURL, bytes.NewReader(body))
			if err != nil {
				return
			}
			req.Header.Set("Content-Type", "application/json")
			resp, err := http.DefaultClient.Do(req)
			if err == nil && resp != nil {
				resp.Body.Close()
			}
		}()
	}
}
