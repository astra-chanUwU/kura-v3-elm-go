package main

import (
	"strings"
	"testing"
)

func TestAccountCommandsRejectPasswordArguments(t *testing.T) {
	cases := [][]string{
		{"reset-password", "--handle", "owner", "secret-password-1"},
		{"reset-password", "--handle", "owner", "--password", "secret-password-1"},
		{"reset-password", "--handle", "owner", "--adopt-legacy", "local"},
		{"bootstrap", "--handle", "owner", "secret-password-1"},
		{"bootstrap", "--password", "secret-password-1"},
		{"create-admin", "--handle", "owner"},
	}
	for _, args := range cases {
		err := execute(args, strings.NewReader("secret-password-1\n"), "postgres://example")
		if err == nil {
			t.Fatalf("%v accepted", args)
		}
		if strings.Contains(err.Error(), "secret-password-1") {
			t.Fatalf("%v error leaked password: %v", args, err)
		}
	}
}

func TestResetPasswordRequiresHandleAndStdin(t *testing.T) {
	if err := execute([]string{"reset-password"}, strings.NewReader("secret-password-1\n"), "postgres://example"); err == nil || !strings.Contains(err.Error(), "handle") {
		t.Fatalf("missing handle err=%v", err)
	}
	if err := execute(nil, strings.NewReader("x"), ""); err == nil || !strings.Contains(err.Error(), "reset-password") {
		t.Fatalf("usage err=%v", err)
	}
	err := execute([]string{"reset-password", "--handle", "owner"}, strings.NewReader("\n"), "postgres://example")
	if err == nil || !strings.Contains(err.Error(), "stdin") {
		t.Fatalf("empty password err=%v", err)
	}
	if err := execute([]string{"reset-password", "--handle", "owner"}, strings.NewReader("secret-password-1\n"), "  "); err == nil || !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatalf("missing database err=%v", err)
	}
}

func TestBootstrapStillRefusesASecondPath(t *testing.T) {
	err := execute([]string{"bootstrap"}, strings.NewReader(""), "")
	if err == nil || !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatalf("bootstrap err=%v", err)
	}
	err = execute([]string{"bootstrap", "--handle", "owner"}, strings.NewReader("\n"), "postgres://example")
	if err == nil || !strings.Contains(err.Error(), "stdin") {
		t.Fatalf("bootstrap empty password err=%v", err)
	}
}
