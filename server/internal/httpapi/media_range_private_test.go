package httpapi

import (
	"bytes"
	"context"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/media"
	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/posts"
)

func TestPrivateMediaRangesAndMetadataOnlyHead(t *testing.T) {
	for _, provider := range []string{"local", "s3"} {
		t.Run(provider, func(t *testing.T) {
			const key = "uploads/video.mp4"
			const reference = "/media/" + key
			const content = "0123456789"
			var gets atomic.Int64
			var store media.Store
			if provider == "local" {
				store = media.NewLocalStore(t.TempDir())
			} else {
				endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path != "/fixture/"+key {
						http.NotFound(w, r)
						return
					}
					if r.Method == http.MethodGet {
						gets.Add(1)
					}
					http.ServeContent(w, r, "", time.Time{}, strings.NewReader(content))
				}))
				t.Cleanup(endpoint.Close)
				remote, err := media.NewS3Store(media.S3Config{Endpoint: endpoint.URL, Bucket: "fixture", URLPrefix: "/media"})
				if err != nil {
					t.Fatal(err)
				}
				store = remote
			}
			if provider == "local" {
				if _, err := store.Put(context.Background(), key, "video/mp4", strings.NewReader(content)); err != nil {
					t.Fatal(err)
				}
			}
			library := &privateLibrary{
				visibility: map[string]posts.MediaVisibility{reference: {Referenced: true, Visible: true}},
				refs:       map[string]storedRef{reference: {key: key, mediaType: "video/mp4"}},
			}
			router, viewer, _ := privateMediaRouter(t, library, store)
			for _, tc := range []struct {
				name, method, byteRange, body, contentRange, length string
				status                                              int
			}{
				{name: "full", method: http.MethodGet, body: content, length: "10", status: 200},
				{name: "bounded", method: http.MethodGet, byteRange: "bytes=2-5", body: "2345", contentRange: "bytes 2-5/10", length: "4", status: 206},
				{name: "suffix", method: http.MethodGet, byteRange: "bytes=-3", body: "789", contentRange: "bytes 7-9/10", length: "3", status: 206},
				{name: "open-ended", method: http.MethodGet, byteRange: "bytes=8-", body: "89", contentRange: "bytes 8-9/10", length: "2", status: 206},
				{name: "unsatisfiable", method: http.MethodGet, byteRange: "bytes=99-", contentRange: "bytes */10", status: 416},
				{name: "head", method: http.MethodHead, length: "10", status: 200},
				{name: "head-range", method: http.MethodHead, byteRange: "bytes=2-5", contentRange: "bytes 2-5/10", length: "4", status: 206},
			} {
				t.Run(tc.name, func(t *testing.T) {
					before := gets.Load()
					req := mediaRequest(tc.method, reference, "", viewer, "")
					req.Header.Set("Range", tc.byteRange)
					rec := httptest.NewRecorder()
					router.ServeHTTP(rec, req)
					if rec.Code != tc.status || rec.Header().Get("Content-Range") != tc.contentRange {
						t.Fatalf("status=%d range=%q body=%q", rec.Code, rec.Header().Get("Content-Range"), rec.Body.String())
					}
					if tc.status != 416 && (rec.Body.String() != tc.body || rec.Header().Get("Content-Length") != tc.length || rec.Header().Get("Accept-Ranges") != "bytes") {
						t.Fatalf("headers=%v body=%q", rec.Header(), rec.Body.String())
					}
					if rec.Header().Get("Cache-Control") != "private, no-store" {
						t.Fatalf("private cache policy lost: %v", rec.Header())
					}
					if tc.method == http.MethodHead && gets.Load() != before {
						t.Fatal("HEAD downloaded object bytes")
					}
				})
			}
			req := mediaRequest(http.MethodGet, reference, "", viewer, "")
			req.Header.Set("Range", "bytes=0-1,8-9")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			kind, params, err := mime.ParseMediaType(rec.Header().Get("Content-Type"))
			if err != nil || kind != "multipart/byteranges" || rec.Code != http.StatusPartialContent {
				t.Fatalf("multipart status=%d headers=%v err=%v", rec.Code, rec.Header(), err)
			}
			parts := multipart.NewReader(bytes.NewReader(rec.Body.Bytes()), params["boundary"])
			for _, want := range []string{"01", "89"} {
				part, err := parts.NextPart()
				if err != nil {
					t.Fatal(err)
				}
				data, err := io.ReadAll(part)
				if err != nil || string(data) != want {
					t.Fatalf("part=%q err=%v", data, err)
				}
			}
			if _, err := parts.NextPart(); err != io.EOF {
				t.Fatalf("unexpected extra multipart content: %v", err)
			}
		})
	}
}
