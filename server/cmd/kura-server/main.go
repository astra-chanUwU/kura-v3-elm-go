package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/auth"
	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/httpapi"
	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/jobs"
	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/media"
	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/posts"
	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/recovery"
)

func main() {
	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	apiToken := os.Getenv("KURA_API_TOKEN")
	privateMode := strictBoolEnv("KURA_PRIVATE_LIBRARY")
	allowHTTPLocal := strictBoolEnv("KURA_AUTH_ALLOW_HTTP_LOCAL")
	trustedOrigin := validTrustedOrigin(os.Getenv("KURA_AUTH_TRUSTED_ORIGIN"))
	if privateMode {
		if strings.TrimSpace(os.Getenv("DATABASE_URL")) == "" {
			log.Fatalf("KURA_PRIVATE_LIBRARY requires DATABASE_URL: private mode fails closed without an account database")
		}
		if _, ok := media.S3ConfigFromEnv(); ok {
			log.Fatalf("KURA_PRIVATE_LIBRARY is incompatible with the S3 media provider: direct external media URLs bypass authentication; use local media storage or disable private mode")
		}
	}

	var dependencies httpapi.Dependencies
	dependencies.Recovery = recovery.NewFromEnv()
	var closeBackend func()
	var worker *jobs.Worker
	workerDone := make(chan error, 1)
	workerRunning := false
	if databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL")); databaseURL != "" {
		pgBackend, err := posts.NewPostgresBackend(ctx, databaseURL)
		if err != nil {
			if privateMode {
				log.Fatalf("postgres backend unavailable in private mode: %v", err)
			}
			log.Printf("postgres backend unavailable: %v", err)
		} else {
			if privateMode {
				if err := pgBackend.Ping(ctx); err != nil {
					log.Fatalf("postgres backend unavailable in private mode: %v", err)
				}
				if pgBackend.UsesExternalMediaProvider() {
					log.Fatalf("KURA_PRIVATE_LIBRARY is incompatible with the S3 media provider: direct external media URLs bypass authentication; use local media storage or disable private mode")
				}
				external, err := pgBackend.HasExternalMediaURLs(ctx)
				if err != nil {
					log.Fatalf("private mode media check failed: %v", err)
				}
				if external {
					log.Fatalf("KURA_PRIVATE_LIBRARY refuses persisted external media URLs: direct external URLs bypass authentication; migrate media to local /media storage or disable private mode")
				}
			}
			accountsCfg := httpapi.NewAccountsConfig(auth.NewService(pgBackend.AccountStore()))
			accountsCfg.PrivateMode = privateMode
			accountsCfg.DeploymentToken = strings.TrimSpace(apiToken)
			accountsCfg.AllowHTTPLocal = allowHTTPLocal
			accountsCfg.TrustedOrigin = trustedOrigin
			accountsCfg.SecureCookies = true
			dependencies = httpapi.Dependencies{
				Posts:               pgBackend,
				PostDetails:         pgBackend,
				Uploads:             pgBackend,
				BrowserImports:      pgBackend,
				Metadata:            pgBackend,
				MetadataRead:        pgBackend,
				Ratings:             pgBackend,
				Tags:                pgBackend,
				TagRevisions:        pgBackend,
				Reactions:           pgBackend,
				SavedSearches:       pgBackend,
				Collections:         pgBackend,
				CollectionSummaries: pgBackend,
				CollectionPosts:     pgBackend,
				CollectionPostMover: pgBackend,
				CollectionManager:   pgBackend,
				Trash:               pgBackend,
				TagSuggestions:      pgBackend,
				TagTaxonomy:         pgBackend,
				Exports:             pgBackend,
				Moderation:          pgBackend,
				ModerationRead:      pgBackend,
				ModerationPreview:   pgBackend,
				Jobs:                pgBackend,
				MediaVisibility:     pgBackend,
				Duplicates:          pgBackend,
				Similarity:          pgBackend,
				SimilarityIndex:     pgBackend,
				Accounts:            &accountsCfg,
				Recovery:            recovery.NewFromEnv(),
			}
			closeBackend = pgBackend.Close
			// One shared pool: the backend owns it, and the job store
			// and variant recorder wrap it. Crash recovery runs before
			// the worker claims so interrupted thumbnails resume.
			if recovered, err := pgBackend.RecoverExpiredJobs(ctx); err != nil {
				log.Printf("derivative recovery failed: %v", err)
			} else if recovered > 0 {
				log.Printf("recovered %d expired derivative jobs", recovered)
			}
			configured, err := jobs.NewWorker(jobs.WorkerConfig{
				Store:    pgBackend.JobsStore(),
				Handler:  pgBackend.HandleMediaJob,
				LockedBy: derivativeWorkerID(),
				Kinds:    []string{media.KindDerivative, media.KindFingerprint},
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
	if dependencies.Recovery != nil {
		if err := dependencies.Recovery.Reconcile(); err != nil {
			log.Printf("backup restart recovery failed")
		}
	}
	if privateMode && (dependencies.Accounts == nil || dependencies.Accounts.Service == nil) {
		log.Fatalf("KURA_PRIVATE_LIBRARY requires an account service: private mode fails closed without it")
	}
	if closeBackend != nil {
		defer closeBackend()
	}

	server := &http.Server{Addr: addr, Handler: httpapi.NewRouterWithDependencies(apiToken, dependencies)}
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

// strictBoolEnv parses an opt-in boolean env var; any non-empty value that
// is not a valid boolean is a fatal configuration error.
func strictBoolEnv(name string) bool {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return false
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		log.Fatalf("%s must be a boolean (true/false): %q", name, raw)
	}
	return v
}

// validTrustedOrigin validates an exact public origin: scheme and host
// only, with no path, query, fragment, or credentials.
func validTrustedOrigin(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil ||
		u.Path != "" || u.RawQuery != "" || u.Fragment != "" || raw != u.Scheme+"://"+u.Host {
		log.Fatalf("KURA_AUTH_TRUSTED_ORIGIN must be an exact origin like https://library.example (no path, query, or credentials)")
	}
	return raw
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
