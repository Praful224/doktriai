package core

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/praful224/doktriai/doktriai-packages"
)

func TestNotifySlack(t *testing.T) {
	// Spin up a test server to simulate Slack's incoming webhook endpoint
	receivedChan := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("failed to read body: %v", err)
		}
		receivedChan <- body
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Update policy configuration for testing
	policy := GetPolicy()
	originalURL := policy.Notifications.SlackWebhookURL
	policy.Notifications.SlackWebhookURL = server.URL
	defer func() {
		policy.Notifications.SlackWebhookURL = originalURL
	}()

	// Construct a test pending plan
	plan := &packages.PendingPlan{
		ID:             "plan-test-1234",
		RequestedBy:    "test-user",
		ExpiresAt:      time.Now().UTC().Add(15 * time.Minute),
		ApprovalReason: "Deploying standard service",
		Spec: packages.WorkloadSpec{
			Name:     "web-nginx",
			Image:    "nginx:alpine",
			Replicas: 3,
			Env: map[string]string{
				"DB_PASSWORD": "secret",
			},
		},
	}

	// Trigger notifier
	NotifySlack(plan, "http://localhost:18080")

	// Wait for delivery
	select {
	case body := <-receivedChan:
		// Parse block response
		var payload struct {
			Text   string `json:"text"`
			Blocks []struct {
				Type string `json:"type"`
				Text *struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"text,omitempty"`
				Elements []struct {
					Type  string `json:"type"`
					Style string `json:"style"`
					Url   string `json:"url"`
				} `json:"elements,omitempty"`
			} `json:"blocks"`
		}

		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("failed to unmarshal JSON: %v", err)
		}

		// Verify title text and blocks
		if !strings.Contains(payload.Text, "PTE Gate") {
			t.Errorf("expected text summary to contain 'PTE Gate', got: %s", payload.Text)
		}

		if len(payload.Blocks) != 2 {
			t.Fatalf("expected 2 blocks (details + actions), got %d", len(payload.Blocks))
		}

		// Assert Details block contains computed fields: workload name, proposed change, risk level
		detailsText := payload.Blocks[0].Text.Text
		if !strings.Contains(detailsText, "web-nginx") {
			t.Errorf("details block should contain workload name, got: %s", detailsText)
		}
		if !strings.Contains(detailsText, "Deploy image `nginx:alpine` with replicas `3`") {
			t.Errorf("details block should contain proposed change, got: %s", detailsText)
		}
		// Since DB_PASS is sensitive (contains PASS), risk level should be HIGH
		if !strings.Contains(detailsText, "HIGH (Sensitive credentials in environment)") {
			t.Errorf("details block should show HIGH risk due to env, got: %s", detailsText)
		}

		// Assert actions buttons are correct
		actions := payload.Blocks[1]
		if len(actions.Elements) != 2 {
			t.Fatalf("expected 2 action buttons, got %d", len(actions.Elements))
		}

		approveBtn := actions.Elements[0]
		if approveBtn.Style != "primary" || !strings.Contains(approveBtn.Url, "approve?token=") {
			t.Errorf("invalid approve button config: %+v", approveBtn)
		}

		rejectBtn := actions.Elements[1]
		if rejectBtn.Style != "danger" || !strings.Contains(rejectBtn.Url, "reject?token=") {
			t.Errorf("invalid reject button config: %+v", rejectBtn)
		}

	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for webhook notification delivery")
	}
}

func TestNotifyDrift(t *testing.T) {
	// Spin up a test server to simulate Slack's incoming webhook endpoint
	receivedChan := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("failed to read body: %v", err)
		}
		receivedChan <- body
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Update policy configuration for testing
	policy := GetPolicy()
	originalURL := policy.Notifications.SlackWebhookURL
	policy.Notifications.SlackWebhookURL = server.URL
	defer func() {
		policy.Notifications.SlackWebhookURL = originalURL
	}()

	// Trigger drift alert notifier
	NotifyDrift("payment-service", 4, 2)

	// Wait for delivery
	select {
	case body := <-receivedChan:
		var payload struct {
			Text   string `json:"text"`
			Blocks []struct {
				Type string `json:"type"`
				Text *struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"text,omitempty"`
			} `json:"blocks"`
		}

		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("failed to unmarshal JSON: %v", err)
		}

		// Verify text details
		if !strings.Contains(payload.Text, "Drift Detected") {
			t.Errorf("expected text summary to contain 'Drift Detected', got: %s", payload.Text)
		}

		if len(payload.Blocks) != 1 {
			t.Fatalf("expected 1 details block, got %d", len(payload.Blocks))
		}

		detailsText := payload.Blocks[0].Text.Text
		if !strings.Contains(detailsText, "payment-service") {
			t.Errorf("details block should contain workload name, got: %s", detailsText)
		}
		if !strings.Contains(detailsText, "Expected Replicas:* `4`") {
			t.Errorf("details block should contain expected count, got: %s", detailsText)
		}
		if !strings.Contains(detailsText, "Actual Replicas:* `2`") {
			t.Errorf("details block should contain actual count, got: %s", detailsText)
		}

	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for drift webhook delivery")
	}
}
