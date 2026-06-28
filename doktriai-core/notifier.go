package core

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
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
		emoji := "🔴"
		text := fmt.Sprintf(
			"%s *PTE Gate — Human Approval Required*\n"+
				"*Workload:* `%s`\n"+
				"*Requested by:* `%s`\n"+
				"*Reason:* %s\n"+
				"*Expires:* <!date^%d^{date_time}|%s>\n\n"+
				"✅ Approve: `POST %s/api/plan/%s/approve`\n"+
				"❌ Reject:  `POST %s/api/plan/%s/reject`",
			emoji, plan.Spec.Name, plan.RequestedBy, plan.ApprovalReason,
			plan.ExpiresAt.Unix(), plan.ExpiresAt.Format(time.RFC3339),
			apiBaseURL, plan.ID, apiBaseURL, plan.ID,
		)
		payload := SlackBlock{Text: text}
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

// NotifyDrift sends a drift alert to the configured drift webhook.
func NotifyDrift(workload string, desired, actual int) {
	url := GetPolicy().Notifications.DriftWebhookURL
	if url == "" {
		return
	}
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
		req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, _ := http.DefaultClient.Do(req)
		if resp != nil {
			resp.Body.Close()
		}
	}()
}
