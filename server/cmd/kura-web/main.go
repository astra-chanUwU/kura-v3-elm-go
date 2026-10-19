package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/httpserver"
)

const (
	webAddress       = "127.0.0.1:8000"
	defaultAPITarget = "http://127.0.0.1:8080"
)

func main() {
	log.Print("Kura frontend: http://localhost:8000")
	handler, err := newWebHandler("../web", os.Getenv("KURA_API_PROXY_URL"))
	if err != nil {
		log.Fatal(err)
	}
	server := &http.Server{
		Addr:              webAddress,
		Handler:           handler,
		ReadHeaderTimeout: httpserver.ReadHeaderTimeout,
		ReadTimeout:       httpserver.ReadTimeout,
		IdleTimeout:       httpserver.IdleTimeout,
		MaxHeaderBytes:    httpserver.MaxHeaderBytes,
	}
	if err := server.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

func newWebHandler(staticDir, configuredTarget string) (http.Handler, error) {
	target, err := validateProxyTarget(configuredTarget)
	if err != nil {
		return nil, err
	}
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 3 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		IdleConnTimeout:       60 * time.Second,
		MaxIdleConns:          20,
		MaxIdleConnsPerHost:   10,
		MaxConnsPerHost:       100,
	}
	proxy := &httputil.ReverseProxy{
		Rewrite: func(request *httputil.ProxyRequest) {
			request.SetURL(target)
			request.Out.Host = request.In.Host
			request.Out.Header.Del("Forwarded")
			request.Out.Header.Del("X-Forwarded-For")
			request.Out.Header.Del("X-Forwarded-Host")
			request.Out.Header.Del("X-Forwarded-Proto")
			request.Out.Header.Del("X-Forwarded-Port")
		},
		Transport: transport,
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
			http.Error(w, "API unavailable", http.StatusBadGateway)
		},
	}
	files := restrictedStaticHandler(staticDir)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isAPIProxyPath(r.URL.Path) {
			proxy.ServeHTTP(w, r)
			return
		}
		files.ServeHTTP(w, r)
	}), nil
}

// restrictedStaticHandler serves only the frontend artifacts the app
// actually references. Everything else under the web root — source,
// tests, dotfiles, build test bundles, and above all the private media
// tree at static/media (the default MEDIA_ROOT, which must only be
// reachable through the API's gated /media proxy) — returns 404. The
// request path is canonicalized before the allowlist check, so encoded
// or dotted variants of a denied path are denied as well.
func restrictedStaticHandler(staticDir string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		cleaned := path.Clean("/" + strings.TrimPrefix(r.URL.Path, "/"))
		if cleaned == "/static/media" || strings.HasPrefix(cleaned, "/static/media/") {
			http.NotFound(w, r)
			return
		}
		rel, ok := allowedStaticFile(cleaned)
		if !ok {
			http.NotFound(w, r)
			return
		}
		root, err := os.OpenRoot(staticDir)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer func() { _ = root.Close() }()
		file, err := root.Open(rel)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer func() { _ = file.Close() }()
		info, err := file.Stat()
		if err != nil || info.IsDir() {
			http.NotFound(w, r)
			return
		}
		http.ServeContent(w, r, info.Name(), info.ModTime(), file)
	})
}

// allowedStaticFile maps a canonical URL path to the file below the web
// root that may be served for it, or reports false to deny the path.
func allowedStaticFile(cleaned string) (string, bool) {
	switch cleaned {
	case "/":
		return "index.html", true
	case "/index.html":
		return "index.html", true
	case "/sign-in", "/sign-up", "/trouble":
		return "index.html", true
	case "/static/brand/auth-observatory.png":
		return "static/brand/auth-observatory.png", true
	case "/dist/elm.js":
		return "dist/elm.js", true
	}
	if rest, ok := strings.CutPrefix(cleaned, "/static/icons/"); ok {
		if rest == "" || strings.Contains(rest, "/") || strings.HasPrefix(rest, ".") || !strings.HasSuffix(rest, ".svg") {
			return "", false
		}
		return "static/icons/" + rest, true
	}
	// Top-level frontend artifacts only (index.html is handled above):
	// kura.css, browser-import.js, moderation-preview.js, and similar
	// plain scripts/styles. Nested source, tests, and data stay denied.
	if strings.Count(cleaned, "/") == 1 {
		name := strings.TrimPrefix(cleaned, "/")
		if name == "" || strings.HasPrefix(name, ".") {
			return "", false
		}
		switch path.Ext(name) {
		case ".js", ".css":
			return name, true
		}
	}
	return "", false
}

func isAPIProxyPath(path string) bool {
	return path == "/api" || strings.HasPrefix(path, "/api/") || path == "/media" || strings.HasPrefix(path, "/media/") || path == "/health"
}

func validateProxyTarget(raw string) (*url.URL, error) {
	if strings.TrimSpace(raw) == "" {
		raw = defaultAPITarget
	}
	target, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || target == nil || (target.Scheme != "http" && target.Scheme != "https") || target.Hostname() == "" || target.User != nil || target.RawQuery != "" || target.ForceQuery || target.Fragment != "" {
		return nil, errors.New("invalid KURA_API_PROXY_URL: use an absolute http:// or https:// URL without credentials, query, or fragment")
	}
	if target.Opaque != "" {
		return nil, errors.New("invalid KURA_API_PROXY_URL: opaque URLs are not supported")
	}
	if target.Port() != "" {
		port, portErr := strconv.Atoi(target.Port())
		if portErr != nil || port < 1 || port > 65535 {
			return nil, errors.New("invalid KURA_API_PROXY_URL: port must be between 1 and 65535")
		}
	}
	if proxyLoopsToWeb(target) {
		return nil, errors.New("invalid KURA_API_PROXY_URL: proxy target points back to the frontend server")
	}
	return target, nil
}

func proxyLoopsToWeb(target *url.URL) bool {
	port := target.Port()
	if port == "" {
		if target.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	if port != "8000" {
		return false
	}
	host := strings.TrimSuffix(strings.ToLower(target.Hostname()), ".")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback() || ip.IsUnspecified()
	}
	// Resolve only the fixed startup target. A request cannot choose a host
	// or cause an additional lookup through the proxy route.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return false
	}
	for _, address := range addresses {
		if address.IP.IsLoopback() {
			return true
		}
	}
	return false
}
