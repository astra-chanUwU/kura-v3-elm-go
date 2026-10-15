package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// readConsolePassword is replaced in tests that exercise the no-echo path.
var readConsolePassword = func(fd int) ([]byte, error) {
	return term.ReadPassword(fd)
}

func stdinInteractive(in io.Reader) bool {
	file, ok := in.(*os.File)
	if !ok || file == nil {
		return false
	}
	return term.IsTerminal(int(file.Fd()))
}

// readPassword reads one password from a terminal without echo, or one line
// from a non-interactive stdin. It never accepts the password from flags.
func readPassword(in io.Reader, stderr io.Writer) (string, error) {
	if stdinInteractive(in) {
		return promptSecret(stderr, func() ([]byte, error) {
			return readConsolePassword(int(in.(*os.File).Fd()))
		})
	}
	if in == nil {
		return "", fmt.Errorf("password is empty")
	}
	line, err := bufio.NewReader(io.LimitReader(in, 1025)).ReadString('\n')
	if err != nil && err != io.EOF {
		return "", fmt.Errorf("read password: %w", err)
	}
	return normalizePassword(line)
}

func promptSecret(stderr io.Writer, readSecret func() ([]byte, error)) (string, error) {
	fmt.Fprint(stderr, "Password: ")
	buf, err := readSecret()
	fmt.Fprintln(stderr)
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	defer func() {
		for i := range buf {
			buf[i] = 0
		}
	}()
	return normalizePassword(string(buf))
}

func normalizePassword(raw string) (string, error) {
	password := strings.TrimRight(raw, "\r\n")
	if password == "" {
		return "", fmt.Errorf("password is empty")
	}
	if len(password) > 1024 {
		return "", fmt.Errorf("password is too long")
	}
	return password, nil
}
