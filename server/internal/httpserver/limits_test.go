package httpserver

import (
	"net/http"
	"testing"
	"time"
)

func TestAPIServerKeepsStreamsOpenAndBoundsHeaders(t *testing.T) {
	server := New(":8080", http.NewServeMux())
	if server.ReadHeaderTimeout != 10*time.Second || server.ReadTimeout != 30*time.Minute || server.IdleTimeout != 2*time.Minute {
		t.Fatalf("timeouts header=%s read=%s idle=%s", server.ReadHeaderTimeout, server.ReadTimeout, server.IdleTimeout)
	}
	if server.WriteTimeout != 0 {
		t.Fatal("write timeout would cut a streamed export")
	}
	if server.MaxHeaderBytes != 1<<20 {
		t.Fatalf("headers = %d", server.MaxHeaderBytes)
	}
}
