package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestPasswordNonInteractiveDoesNotEcho(t *testing.T) {
	password := "viewer-password-1"
	var stderr bytes.Buffer
	got, err := readPassword(strings.NewReader(password+"\n"), &stderr)
	if err != nil || got != password {
		t.Fatalf("got %q err %v", got, err)
	}
	if strings.Contains(stderr.String(), password) {
		t.Fatalf("stderr echoed password: %q", stderr.String())
	}
	if _, err := readPassword(strings.NewReader("\n"), &stderr); err == nil || err.Error() != "password is empty" {
		t.Fatalf("empty password err=%v", err)
	}
}

func TestPasswordPromptDoesNotEcho(t *testing.T) {
	password := "editor-password-1"
	var stderr bytes.Buffer
	got, err := promptSecret(&stderr, func() ([]byte, error) {
		return []byte(password), nil
	})
	if err != nil || got != password {
		t.Fatalf("got %q err %v", got, err)
	}
	if !strings.Contains(stderr.String(), "Password:") || strings.Contains(stderr.String(), password) {
		t.Fatalf("prompt stderr=%q", stderr.String())
	}
}

func TestCredentialFileIsUserPrivate(t *testing.T) {
	path := t.TempDir() + string(os.PathSeparator) + "credentials.json"
	secret := "viewer-session-token-1"
	body := []byte("{\n  \"cookie\": \"" + secret + "\"\n}\n")
	if err := writePrivateFile(path, body); err != nil {
		t.Fatal(err)
	}
	if err := assertUserPrivate(path); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, body) {
		t.Fatalf("read private file: %v", err)
	}
	loose := t.TempDir() + string(os.PathSeparator) + "loose.json"
	if err := os.WriteFile(loose, body, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := assertUserPrivate(loose); err == nil {
		t.Fatal("loose credential file was accepted")
	}
}
