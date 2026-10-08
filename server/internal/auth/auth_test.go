package auth

import (
	"strings"
	"testing"
)

func TestDeploymentTokenMapsToSystemActor(t *testing.T) {
	actor, ok := ResolveDeploymentActor("secret", "secret")
	if !ok || actor.ID != SystemActorID || !actor.System {
		t.Fatalf("deployment token actor=%#v ok=%v", actor, ok)
	}
	if _, ok := ResolveDeploymentActor("secret", "wrong"); ok {
		t.Fatal("wrong token resolved to an actor")
	}
}

func TestEmptyTokenPreservesLocalActor(t *testing.T) {
	actor, ok := ResolveDeploymentActor("", "")
	if !ok || actor.ID != LocalActorID {
		t.Fatalf("local actor=%#v ok=%v", actor, ok)
	}
}

func TestSessionLookupResolvesUserActor(t *testing.T) {
	lookup := func(hash string) (Session, bool) {
		if hash == HashToken("session-token") {
			return Session{TokenHash: hash, UserID: 7, ActorID: "user-7"}, true
		}
		return Session{}, false
	}
	actor, ok := ResolveActor("secret", "session-token", lookup)
	if !ok || actor.ID != "user-7" || actor.UserID != 7 || actor.System {
		t.Fatalf("session actor=%#v ok=%v", actor, ok)
	}
	if _, ok := ResolveActor("secret", "unknown", lookup); ok {
		t.Fatal("unknown session token resolved to an actor")
	}
	if _, ok := ResolveActor("secret", "", lookup); ok {
		t.Fatal("empty token resolved to an actor")
	}
}

func TestDeploymentTokenTakesPrecedenceOverSession(t *testing.T) {
	lookup := func(string) (Session, bool) {
		return Session{ActorID: "user-9", UserID: 9}, true
	}
	actor, ok := ResolveActor("secret", "secret", lookup)
	if !ok || actor.ID != SystemActorID {
		t.Fatalf("precedence actor=%#v ok=%v", actor, ok)
	}
}

func TestWriteCapabilityRequiresAuthenticatedActor(t *testing.T) {
	if !HasWriteCapability(Actor{ID: SystemActorID, System: true}) {
		t.Fatal("system actor lacks write capability")
	}
	if !HasWriteCapability(Actor{ID: LocalActorID}) {
		t.Fatal("local actor lacks write capability")
	}
	if HasWriteCapability(Actor{}) {
		t.Fatal("empty actor holds write capability")
	}
}

func TestDefaultPolicyKeepsSystemWriteAndDeniesUnknown(t *testing.T) {
	policy := DefaultPolicy()
	if !policy.Can(SystemActorID, WriteCapability, "", "") {
		t.Fatal("system actor lost global write capability")
	}
	if policy.Can("ghost", WriteCapability, "", "") {
		t.Fatal("unknown actor holds global write capability")
	}
	if policy.Can("", WriteCapability, "", "") || policy.Can(SystemActorID, "", "", "") {
		t.Fatal("empty holder or capability passed")
	}
}

func TestPerResourceGrantScopesAccess(t *testing.T) {
	policy := DefaultPolicy()
	policy.Grants = []Grant{{
		HolderID:     "user-7",
		Capability:   WriteCapability,
		ResourceType: "collection",
		ResourceID:   "42",
	}}
	if !policy.Can("user-7", WriteCapability, "collection", "42") {
		t.Fatal("scoped grant denied on its own resource")
	}
	if policy.Can("user-7", WriteCapability, "collection", "43") {
		t.Fatal("scoped grant leaked to another resource")
	}
	if policy.Can("user-7", WriteCapability, "", "") {
		t.Fatal("scoped grant leaked to global scope")
	}
}

func TestHashTokenIsStableAndBearerParsing(t *testing.T) {
	if HashToken("abc") != HashToken("abc") || strings.TrimSpace(HashToken("abc")) == "" {
		t.Fatal("session token hash is unstable or blank")
	}
	header := "Bearer" + " " + "tok" + "en-123"
	if got := BearerToken(header); got != "tok"+"en-123" {
		t.Fatalf("bearer parse=%q", got)
	}
	if BearerToken("Basic abc") != "" {
		t.Fatal("non-bearer scheme parsed as token")
	}
}
