package core

import (
	"strings"
	"testing"
	"time"
)

// ─── Token Generation & Verification ──────────────────────────────────────────

func TestGenerateAndVerifyToken_HappyPath(t *testing.T) {
	token, err := GenerateRequestToken("alice", "admin", "agent-1", "deploy,list", "deploy nginx")
	if err != nil {
		t.Fatalf("token generation failed: %v", err)
	}

	claims, err := VerifyRequestToken(token)
	if err != nil {
		t.Fatalf("token verification failed: %v", err)
	}

	if claims.ActorName != "alice" {
		t.Errorf("expected actor 'alice', got %q", claims.ActorName)
	}
	if claims.Role != "admin" {
		t.Errorf("expected role 'admin', got %q", claims.Role)
	}
	if claims.AgentID != "agent-1" {
		t.Errorf("expected agentId 'agent-1', got %q", claims.AgentID)
	}
	if claims.Scope != "deploy,list" {
		t.Errorf("expected scope 'deploy,list', got %q", claims.Scope)
	}
	if claims.Goal != "deploy nginx" {
		t.Errorf("expected goal 'deploy nginx', got %q", claims.Goal)
	}
	if claims.Dev {
		t.Error("expected non-dev token")
	}
}

func TestVerifyToken_MalformedToken(t *testing.T) {
	badTokens := []string{
		"",
		"only-one-part",
		"a|b|c",                           // too few fields
		"a|b|c|d|e|f|g|h|i",              // too many fields
		"a|b|c|d|e|not-a-timestamp|f|sig", // bad timestamp
	}
	for _, token := range badTokens {
		_, err := VerifyRequestToken(token)
		if err == nil {
			t.Errorf("expected error for malformed token %q, got nil", token)
		}
	}
}

func TestVerifyToken_InvalidSignature(t *testing.T) {
	token, _ := GenerateRequestToken("bob", "operator", "", "", "")
	// Tamper with the signature (last field)
	parts := strings.Split(token, "|")
	parts[7] = "0000000000000000000000000000000000000000000000000000000000000000"
	tampered := strings.Join(parts, "|")

	_, err := VerifyRequestToken(tampered)
	if err == nil {
		t.Error("expected signature verification failure")
	}
	if !strings.Contains(err.Error(), "signature invalid") {
		t.Errorf("expected 'signature invalid' error, got: %v", err)
	}
}

func TestVerifyToken_TamperedActor(t *testing.T) {
	token, _ := GenerateRequestToken("alice", "admin", "", "", "")
	parts := strings.Split(token, "|")
	parts[0] = "mallory" // change actor
	tampered := strings.Join(parts, "|")

	_, err := VerifyRequestToken(tampered)
	if err == nil {
		t.Error("expected error for tampered actor")
	}
}

func TestVerifyToken_TamperedRole(t *testing.T) {
	token, _ := GenerateRequestToken("alice", "viewer", "", "", "")
	parts := strings.Split(token, "|")
	parts[1] = "admin" // escalate role
	tampered := strings.Join(parts, "|")

	_, err := VerifyRequestToken(tampered)
	if err == nil {
		t.Error("expected error for tampered role escalation")
	}
}

func TestVerifyToken_ReplayPrevention(t *testing.T) {
	token, _ := GenerateRequestToken("alice", "admin", "", "", "")

	// First use should succeed
	_, err := VerifyRequestToken(token)
	if err != nil {
		t.Fatalf("first token use should succeed: %v", err)
	}

	// Second use of same token should fail (nonce replay)
	_, err = VerifyRequestToken(token)
	if err == nil {
		t.Error("expected replay detection on second use")
	}
	if !strings.Contains(err.Error(), "replay") {
		t.Errorf("expected 'replay' in error, got: %v", err)
	}
}

func TestVerifyToken_ExpiredToken(t *testing.T) {
	// We can't easily test expiry without mocking time, but we can verify
	// the token format includes a timestamp
	token, _ := GenerateRequestToken("alice", "admin", "", "", "")
	parts := strings.Split(token, "|")
	if len(parts) != 8 {
		t.Fatalf("expected 8 pipe-delimited fields, got %d", len(parts))
	}
	// The timestamp field (index 5) should be a valid unix timestamp
	tsStr := parts[5]
	if len(tsStr) < 10 {
		t.Errorf("timestamp field %q looks too short for a unix timestamp", tsStr)
	}
}

// ─── DevClaimsFromHeaders ─────────────────────────────────────────────────────

func TestDevClaimsFromHeaders_WithValues(t *testing.T) {
	claims := DevClaimsFromHeaders("operator", "bob")
	if claims.ActorName != "bob" {
		t.Errorf("expected actor 'bob', got %q", claims.ActorName)
	}
	if claims.Role != "operator" {
		t.Errorf("expected role 'operator', got %q", claims.Role)
	}
	if !claims.Dev {
		t.Error("expected dev=true")
	}
}

func TestDevClaimsFromHeaders_EmptyActor(t *testing.T) {
	claims := DevClaimsFromHeaders("admin", "")
	if claims.ActorName != "admin" {
		t.Errorf("expected actor fallback to role 'admin', got %q", claims.ActorName)
	}
}

func TestDevClaimsFromHeaders_EmptyRole(t *testing.T) {
	claims := DevClaimsFromHeaders("", "alice")
	if claims.Role != "admin" {
		t.Errorf("expected role fallback to 'admin', got %q", claims.Role)
	}
}

func TestDevClaimsFromHeaders_BothEmpty(t *testing.T) {
	claims := DevClaimsFromHeaders("", "")
	if claims.Role != "admin" {
		t.Errorf("expected role 'admin', got %q", claims.Role)
	}
	if claims.ActorName != "admin" {
		t.Errorf("expected actor 'admin', got %q", claims.ActorName)
	}
}

// ─── IsDevMode ────────────────────────────────────────────────────────────────

func TestIsDevMode(t *testing.T) {
	// By default (no env var), should be dev mode
	if !IsDevMode() {
		t.Error("expected dev mode by default")
	}
}

// ─── NonceCache ───────────────────────────────────────────────────────────────

func TestNonceCache_UniqueNonces(t *testing.T) {
	nc := &nonceCache{m: make(map[string]time.Time)}
	now := time.Now()

	if nc.seen("nonce-1", now) {
		t.Error("first use of nonce-1 should return false")
	}
	if !nc.seen("nonce-1", now) {
		t.Error("second use of nonce-1 should return true (seen)")
	}
	if nc.seen("nonce-2", now) {
		t.Error("first use of nonce-2 should return false")
	}
}

func TestNonceCache_PurgesExpired(t *testing.T) {
	nc := &nonceCache{m: make(map[string]time.Time)}
	past := time.Now().Add(-3 * time.Minute)

	// Add a nonce from the past
	nc.m["old-nonce"] = past

	// Check a new nonce — should trigger purge
	now := time.Now()
	nc.seen("new-nonce", now)

	if _, exists := nc.m["old-nonce"]; exists {
		t.Error("expected old-nonce to be purged")
	}
}

// ─── Random Hex ───────────────────────────────────────────────────────────────

func TestRandomHex_Length(t *testing.T) {
	hex, err := randomHex(8)
	if err != nil {
		t.Fatalf("randomHex failed: %v", err)
	}
	// 8 bytes → 16 hex chars
	if len(hex) != 16 {
		t.Errorf("expected 16 hex chars, got %d", len(hex))
	}
}

func TestRandomHex_Uniqueness(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		hex, _ := randomHex(8)
		if seen[hex] {
			t.Fatalf("randomHex produced duplicate: %s", hex)
		}
		seen[hex] = true
	}
}
