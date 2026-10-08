// Package auth is the durable identity and capability foundation.
//
// Actor identity: every authenticated request resolves to an Actor whose ID
// is the canonical capability-owner key. The two legacy IDs, "local" (local
// development without a deployment token) and "system" (requests bearing
// the deployment KURA_API_TOKEN bearer token), stay valid so existing
// saved-search owner_actor rows keep working. No passwords, OAuth, WebAuthn,
// or external provider is introduced here.
package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"strings"
)

// Legacy actor IDs preserved from the pre-session bearer-token boundary.
const (
	LocalActorID  = "local"
	SystemActorID = "system"
)

// Capability names seeded by db/migrations/008_identity.sql.
const WriteCapability = "write"

// Actor identifies the capability owner for a request. UserID is the
// durable users.id once session lookup backing exists; it is zero for the
// deployment-token actors, which carry no session.
type Actor struct {
	ID     string
	UserID int64
	System bool
}

// Session is the durable session backing for a user actor.
type Session struct {
	TokenHash string
	UserID    int64
	ActorID   string
}

// SessionLookup resolves a session by its token hash. A nil lookup (or a
// lookup returning false) means no session matched.
type SessionLookup func(tokenHash string) (Session, bool)

// ResolveDeploymentActor maps the configured deployment token and the
// request's presented bearer token to the explicit system actor, or to the
// local actor when no deployment token is configured. Session tokens never
// resolve here; use ResolveActor for session-aware resolution.
func ResolveDeploymentActor(expected, provided string) (Actor, bool) {
	if expected == "" {
		return Actor{ID: LocalActorID}, true
	}
	if SameToken(provided, expected) {
		return Actor{ID: SystemActorID, System: true}, true
	}
	return Actor{}, false
}

// ResolveActor resolves identity in precedence order: the deployment bearer
// token first (system actor), then a session token via lookup. An empty
// expected token preserves local development as the local actor without
// consulting sessions.
func ResolveActor(expected, provided string, lookup SessionLookup) (Actor, bool) {
	if expected == "" {
		return Actor{ID: LocalActorID}, true
	}
	if SameToken(provided, expected) {
		return Actor{ID: SystemActorID, System: true}, true
	}
	if lookup == nil || strings.TrimSpace(provided) == "" {
		return Actor{}, false
	}
	session, ok := lookup(HashToken(provided))
	if !ok || strings.TrimSpace(session.ActorID) == "" {
		return Actor{}, false
	}
	return Actor{ID: session.ActorID, UserID: session.UserID}, true
}

// HasWriteCapability reports whether actor may perform API mutations. The
// system actor always holds the write capability; every other resolved actor
// holds it once authenticated. Unauthenticated (empty-ID) actors never do.
func HasWriteCapability(actor Actor) bool {
	return strings.TrimSpace(actor.ID) != ""
}

// SameToken compares bearer tokens in constant time without leaking length
// information through early exit.
func SameToken(provided, expected string) bool {
	providedHash := sha256.Sum256([]byte(provided))
	expectedHash := sha256.Sum256([]byte(expected))
	return subtle.ConstantTimeCompare(providedHash[:], expectedHash[:]) == 1
}

// HashToken hashes a session token for storage and lookup comparison.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// BearerToken extracts the token from an Authorization header value.
func BearerToken(header string) string {
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return parts[1]
}
