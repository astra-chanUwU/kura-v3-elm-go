package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
)

const defaultAPIURL = "http://localhost:8080"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := runContext(ctx, os.Args[1:], os.Stdout, os.Stderr, os.Getenv)
	stop()
	os.Exit(code)
}

func run(args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	return runContext(context.Background(), args, stdout, stderr, getenv)
}

func runContext(ctx context.Context, args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	out := &checkedWriter{writer: stdout}
	diagnostics := &checkedWriter{writer: stderr}
	code := dispatch(ctx, args, out, diagnostics, getenv)
	if out.err != nil {
		if diagnostics.written == 0 {
			fmt.Fprintf(stderr, "error: write output: %v\n", out.err)
		}
		return 1
	}
	if diagnostics.err != nil {
		return 1
	}
	return code
}

func dispatch(ctx context.Context, args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	if len(args) == 0 {
		fmt.Fprint(stdout, rootHelp)
		return 0
	}
	switch args[0] {
	case "-h", "--help", "help":
		fmt.Fprint(stdout, rootHelp)
		return 0
	case "search":
		return runSearch(ctx, args[1:], stdout, stderr, getenv)
	case "collection":
		return runCollection(ctx, args[1:], stdout, stderr, getenv)
	case "post":
		return runPost(ctx, args[1:], stdout, stderr, getenv)
	default:
		fmt.Fprintf(stderr, "error: unknown command %q\n", args[0])
		fmt.Fprint(stderr, rootHelp)
		return 1
	}
}
