package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/httpapi"
	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/jobs"
	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/media"
	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/posts"
)

func main() {
	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var searcher posts.Searcher
	var closeSearcher func()
	var worker *jobs.Worker
	workerDone := make(chan error, 1)
	workerRunning := false
	if databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL")); databaseURL != "" {
		pgSearcher, err := posts.NewPostgresSearcher(ctx, databaseURL)
		if err != nil {
			log.Printf("postgres search unavailable: %v", err)
		} else {
			searcher = pgSearcher
			closeSearcher = pgSearcher.Close
			// One shared pool: the searcher owns it, and the job store
			// and variant recorder wrap it. Crash recovery runs before
			// the worker claims so interrupted thumbnails resume.
			if recovered, err := pgSearcher.RecoverExpiredJobs(ctx); err != nil {
				log.Printf("derivative recovery failed: %v", err)
			} else if recovered > 0 {
				log.Printf("recovered %d expired derivative jobs", recovered)
			}
			configured, err := jobs.NewWorker(jobs.WorkerConfig{
				Store:    pgSearcher.JobsStore(),
				Handler:  pgSearcher.NewDerivativeProcessor().Handle,
				LockedBy: derivativeWorkerID(),
				Kinds:    []string{media.KindDerivative},
			})
			if err != nil {
				log.Printf("derivative worker unavailable: %v", err)
			} else {
				worker = configured
				workerRunning = true
				go func() {
					workerDone <- worker.Run(ctx)
				}()
			}
		}
	}
	if closeSearcher != nil {
		defer closeSearcher()
	}

	server := &http.Server{Addr: addr, Handler: httpapi.NewRouterWithToken(os.Getenv("KURA_API_TOKEN"), searcher)}
	go func() {
		log.Printf("kura-server listening on %s", addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("http server error: %v", err)
			stop()
		}
	}()

	// Observe the worker before the shutdown signal: a worker that exits
	// first (unrecoverable store outage after bounded retries) must stop
	// the server promptly instead of leaving HTTP accepting uploads no
	// worker will process. A failed worker is fatal to the process so the
	// platform restarts it; a clean signal shutdown stays exit 0.
	var workerErr error
	workerExited := false
	if workerRunning {
		select {
		case <-ctx.Done():
		case err := <-workerDone:
			workerExited = true
			switch {
			case err != nil:
				log.Printf("derivative worker failed: %v", err)
				workerErr = err
			case ctx.Err() == nil:
				log.Printf("derivative worker stopped unexpectedly; shutting down")
				workerErr = errors.New("derivative worker stopped unexpectedly")
			}
			stop()
		}
	} else {
		<-ctx.Done()
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("http shutdown error: %v", err)
	}
	if workerRunning && !workerExited {
		if err := <-workerDone; err != nil {
			log.Printf("derivative worker stopped: %v", err)
			workerErr = err
		}
	}
	if workerErr != nil {
		os.Exit(1)
	}
}

// derivativeWorkerID identifies this process's worker on claimed job rows.
// It is unique per start so a restarted process cannot complete rows its
// predecessor lost: stale owners get lease conflicts instead.
func derivativeWorkerID() string {
	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return fmt.Sprintf("derivative-%d-%d", os.Getpid(), time.Now().UnixNano())
	}
	return fmt.Sprintf("derivative-%d-%s", os.Getpid(), hex.EncodeToString(nonce[:]))
}
