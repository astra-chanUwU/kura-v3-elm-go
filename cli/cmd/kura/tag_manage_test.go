package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTagManagementDefaultsToPreviewAndAppliesOnlyWithVersion(t *testing.T) {
	var previews, applies int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags/manage/preview":
			previews++
			fmt.Fprint(w, `{"version":0,"mode":"rename","source_tag":"cat","target_tag":"feline","affected_post_count":4,"visible_post_count":3,"target_post_count":0}`)
		case "/api/tags/manage/apply":
			applies++
			fmt.Fprint(w, `{"id":"1","version":1,"mode":"rename","source_tag":"cat","target_tag":"feline","affected_post_count":4,"visible_post_count":3,"changed":true}`)
		default:
			t.Errorf("unexpected request path %q", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	code, out, errOut := runWithEnv([]string{"tag", "rename", "cat", "feline"}, map[string]string{"KURA_API_URL": srv.URL})
	if code != 0 || !strings.Contains(out, "preview") || errOut != "" || previews != 1 || applies != 0 {
		t.Fatalf("default preview: code=%d out=%q err=%q previews=%d applies=%d", code, out, errOut, previews, applies)
	}
	code, out, errOut = runWithEnv([]string{"tag", "rename", "--version", "0", "cat", "feline"}, map[string]string{"KURA_API_URL": srv.URL})
	if code != 0 || !strings.Contains(out, "changed:true") || errOut != "" || previews != 1 || applies != 1 {
		t.Fatalf("explicit apply: code=%d out=%q err=%q previews=%d applies=%d", code, out, errOut, previews, applies)
	}
}

func TestTagManagementRejectsInvalidCommandsBeforeRequest(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests++ }))
	defer srv.Close()
	for _, args := range [][]string{
		{"tag", "rename", "--unknown", "cat", "feline"},
		{"tag", "rename", "--preview", "--version", "0", "cat", "feline"},
		{"tag", "rename", "--version", "-1", "cat", "feline"},
		{"tag", "rename", "cat", "feline", "extra"},
		{"tag", "list", "extra"},
	} {
		code, _, errOut := runWithEnv(args, map[string]string{"KURA_API_URL": srv.URL})
		if code == 0 || !strings.Contains(errOut, "error:") {
			t.Fatalf("accepted %v: code=%d err=%q", args, code, errOut)
		}
	}
	if requests != 0 {
		t.Fatalf("invalid commands sent %d requests", requests)
	}
}
