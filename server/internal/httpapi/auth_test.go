package httpapi

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteCapabilityProtectsMutationsButKeepsReadsPublic(t *testing.T) {
	fake := &fakeCollections{}
	router := NewRouterWithToken("secret", fake)

	read := httptest.NewRequest(http.MethodGet, "/api/posts", nil)
	readResponse := httptest.NewRecorder()
	router.ServeHTTP(readResponse, read)
	if readResponse.Code != http.StatusOK {
		t.Fatalf("public read status=%d body=%s", readResponse.Code, readResponse.Body.String())
	}

	body := bytes.NewBufferString(`{"name":"Private"}`)
	unauthorized := httptest.NewRequest(http.MethodPost, "/api/collections", body)
	unauthorized.Header.Set("Content-Type", "application/json")
	unauthorizedResponse := httptest.NewRecorder()
	router.ServeHTTP(unauthorizedResponse, unauthorized)
	if unauthorizedResponse.Code != http.StatusUnauthorized {
		t.Fatalf("missing token status=%d body=%s", unauthorizedResponse.Code, unauthorizedResponse.Body.String())
	}
	if unauthorizedResponse.Header().Get("WWW-Authenticate") == "" {
		t.Fatal("missing WWW-Authenticate challenge")
	}

	authorized := httptest.NewRequest(http.MethodPost, "/api/collections", bytes.NewBufferString(`{"name":"Private"}`))
	authorized.Header.Set("Content-Type", "application/json")
	authorized.Header.Set("Authorization", "Bearer secret")
	authorizedResponse := httptest.NewRecorder()
	router.ServeHTTP(authorizedResponse, authorized)
	if authorizedResponse.Code != http.StatusCreated || fake.created.Name != "Private" {
		t.Fatalf("authorized mutation status=%d created=%#v", authorizedResponse.Code, fake.created)
	}
}

func TestBearerTokenParsing(t *testing.T) {
	for header, want := range map[string]string{
		"Bearer abc": "abc",
		"bearer abc": "abc",
		"Basic abc":  "",
		"Bearer":     "",
		"Bearer a b": "",
	} {
		if got := bearerToken(header); got != want {
			t.Errorf("bearerToken(%q)=%q, want %q", header, got, want)
		}
	}
}

func TestSameTokenDoesNotDependOnInputLength(t *testing.T) {
	if !sameToken("secret", "secret") {
		t.Fatal("same token rejected")
	}
	if sameToken("secret", "other") || sameToken("short", "much longer") {
		t.Fatal("different tokens accepted")
	}
}

func TestCapabilityModeDoesNotAffectHealth(t *testing.T) {
	router := NewRouterWithToken("secret")
	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("health status=%d", response.Code)
	}
}
