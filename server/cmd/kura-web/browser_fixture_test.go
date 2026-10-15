package main

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestBrowserFixture serves the real frontend/proxy on an ephemeral port
// for an explicitly requested local browser drill. It never uses the dev
// API or database. Removing the output marker stops the bounded fixture.
func TestBrowserFixture(t *testing.T) {
	output, target := os.Getenv("KURA_UI_TEST_OUTPUT"), os.Getenv("KURA_UI_TEST_API")
	if output == "" || target == "" {
		t.Skip("explicit disposable browser fixture not requested")
	}
	if !filepath.IsAbs(output) {
		t.Fatal("fixture output must be absolute")
	}
	root, err := filepath.Abs("../../../web")
	if err != nil {
		t.Fatal(err)
	}
	handler, err := newWebHandler(root, target)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	if err := os.WriteFile(output, []byte(server.URL), 0o600); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(output)
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.NewTimer(20 * time.Minute)
	defer timeout.Stop()
	for {
		select {
		case <-ticker.C:
			if _, err := os.Stat(output); os.IsNotExist(err) {
				return
			}
		case <-timeout.C:
			t.Fatal("disposable browser fixture timed out")
		}
	}
}
