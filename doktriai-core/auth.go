package core

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// authMode controls whether strict token auth or dev-mode header fallback is used.
// Set DOKTRIAI_AUTH_MODE=production to enforce token verification.
var authMode = func() string {
	if v := os.Getenv("DOKTRIAI_AUTH_MODE"); v != "" {
		return v
	}
	return "dev"
}()

// signingSecret is loaded from DOKTRIAI_SIGNING_SECRET env var.
// In dev mode a static fallback is used so local testing still works.
var signingSecret = func() string {
	if v := os.Getenv("DOKTRIAI_SIGNING_SECRET"); v != "" {
		return v
	}
	return "doktriai-dev-secret-do-not-use-in-production"
}()

// nonceWindow is how long a nonce is remembered to prevent replay attacks.
const nonceWindow = 2 * time.Minute

// tokenExpiry is the maximum age of a signed token.
const tokenExpiry = 60 * time.Second

// nonceStore keeps used nonces to defeat replay attacks (ASI07).
var nonceStore = &nonceCache{m: make(map[string]time.Time)}

type nonceCache struct {
	mu sync.Mutex
	m  map[string]time.Time
}

// seen returns true if the nonce was already used within the replay window.
func (nc *nonceCache) seen(nonce string, now time.Time) bool {
	nc.mu.Lock()
	defer nc.mu.Unlock()

	// Purge expired nonces to prevent unbounded growth
	for k, t := range nc.m {
		if now.Sub(t) > nonceWindow {
			delete(nc.m, k)
		}
	}

	if _, exists := nc.m[nonce]; exists {
		return true
	}
	nc.m[nonce] = now
	return false
}

// AgentClaims holds the verified identity extracted from a request token.
type AgentClaims struct {
	ActorName string
	Role      string
	AgentID   string
	Scope     string
	Goal      string
	Dev       bool // true when operating in dev-mode fallback
}

// VerifyRequestToken validates an HMAC-SHA256 signed token.
//
// Token format (pipe-delimited, then HMAC over the whole prefix):
//
//	<actor>|<role>|<agentId>|<scope>|<goal>|<timestamp-unix>|<nonce>|<hmac-hex>
//
// Returns AgentClaims on success, error on failure.
func VerifyRequestToken(tokenStr string) (AgentClaims, error) {
	parts := strings.Split(tokenStr, "|")
	if len(parts) != 8 {
		return AgentClaims{}, fmt.Errorf("malformed token: expected 8 fields, got %d", len(parts))
	}

	actor, role, agentID, scope, goal, tsStr, nonce, sigHex := parts[0], parts[1], parts[2], parts[3], parts[4], parts[5], parts[6], parts[7]

	// --- 1. Verify HMAC signature ---
	mac := hmac.New(sha256.New, []byte(signingSecret))
	payload := strings.Join(parts[:7], "|") // everything except the sig
	mac.Write([]byte(payload))
	expected := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(sigHex), []byte(expected)) {
		return AgentClaims{}, fmt.Errorf("token signature invalid")
	}

	// --- 2. Verify timestamp expiry ---
	var tsUnix int64
	if _, err := fmt.Sscanf(tsStr, "%d", &tsUnix); err != nil {
		return AgentClaims{}, fmt.Errorf("token timestamp malformed")
	}
	tokenTime := time.Unix(tsUnix, 0)
	now := time.Now().UTC()
	if now.Sub(tokenTime) > tokenExpiry {
		return AgentClaims{}, fmt.Errorf("token expired (issued %s ago)", now.Sub(tokenTime).Round(time.Second))
	}
	if tokenTime.After(now.Add(10 * time.Second)) {
		return AgentClaims{}, fmt.Errorf("token issued in the future — clock skew or forgery")
	}

	// --- 3. Replay attack prevention (nonce uniqueness check) ---
	if nonceStore.seen(nonce, now) {
		return AgentClaims{}, fmt.Errorf("token nonce already used — replay attack detected")
	}

	return AgentClaims{
		ActorName: actor,
		Role:      role,
		AgentID:   agentID,
		Scope:     scope,
		Goal:      goal,
		Dev:       false,
	}, nil
}

// GenerateRequestToken mints a signed token for the given identity.
// Used by CLI and test helpers.
func GenerateRequestToken(actor, role, agentID, scope, goal string) (string, error) {
	nonce, err := randomHex(8)
	if err != nil {
		return "", err
	}
	ts := fmt.Sprintf("%d", time.Now().UTC().Unix())
	payload := strings.Join([]string{actor, role, agentID, scope, goal, ts, nonce}, "|")

	mac := hmac.New(sha256.New, []byte(signingSecret))
	mac.Write([]byte(payload))
	sig := hex.EncodeToString(mac.Sum(nil))

	return payload + "|" + sig, nil
}

// IsDevMode returns true when the server is running in dev authentication mode.
func IsDevMode() bool {
	return authMode == "dev"
}

// DevClaimsFromHeaders builds an AgentClaims from legacy X-Doktri-Role / X-Doktri-Actor headers.
// Only used in dev mode to preserve backward compatibility.
func DevClaimsFromHeaders(role, actor string) AgentClaims {
	if role == "" {
		role = "admin"
	}
	if actor == "" {
		actor = role
	}
	return AgentClaims{
		ActorName: actor,
		Role:      role,
		Dev:       true,
	}
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// IssueAgentJWT mints a signed JWT for a specific agent identity.
func IssueAgentJWT(agentID, actorName, role, scope string, ttl time.Duration) (string, error) {
	claims := jwt.MapClaims{
		"sub":   agentID,
		"actor": actorName,
		"role":  role,
		"scope": scope,
		"iat":   time.Now().Unix(),
		"exp":   time.Now().Add(ttl).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(signingSecret))
}

// VerifyAgentJWT validates and parses an agent JWT.
func VerifyAgentJWT(tokenStr string) (AgentClaims, error) {
	token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(signingSecret), nil
	})
	if err != nil {
		return AgentClaims{}, err
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return AgentClaims{}, fmt.Errorf("invalid jwt token")
	}
	
	// Safely extract claims
	actor, _ := claims["actor"].(string)
	role, _ := claims["role"].(string)
	agentID, _ := claims["sub"].(string)
	scope, _ := claims["scope"].(string)

	return AgentClaims{
		ActorName: actor,
		Role:      role,
		AgentID:   agentID,
		Scope:     scope,
		Dev:       false,
	}, nil
}
