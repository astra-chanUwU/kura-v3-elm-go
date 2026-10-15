package httpserver

import (
	"net/http"
	"time"
)

const (
	// ReadHeaderTimeout bounds slow request headers.
	ReadHeaderTimeout = 10 * time.Second
	// ReadTimeout bounds an upload body without a short deadline that would
	// reject a large file on a slow link. The upload handler still rejects
	// bodies above its own size cap.
	ReadTimeout = 30 * time.Minute
	// IdleTimeout closes unused keep-alive connections.
	IdleTimeout = 2 * time.Minute
	// MaxHeaderBytes rejects oversized header blocks.
	MaxHeaderBytes = 1 << 20
)

// New returns the API server with deliberate resource limits.
// WriteTimeout stays unset so a streamed media response or ZIP export is
// not cut off by a single write deadline.
func New(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: ReadHeaderTimeout,
		ReadTimeout:       ReadTimeout,
		IdleTimeout:       IdleTimeout,
		MaxHeaderBytes:    MaxHeaderBytes,
	}
}
