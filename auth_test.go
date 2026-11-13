package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gws prints API errors as JSON on stdout. This is the 2026-11-13 body from an
// expired personal login, with stderr holding only the keyring line.
const expiredStdout = `{"error":{"code":401,"message":"Authentication failed: Failed to get token: Server error: invalid_grant: Token has been expired or revoked.: invalid_grant: Token has been expired or revoked.","reason":"authError"}}`
const keyringStderr = "Using keyring backend: file\n"

// TestMain lets a test run the real main() in a child process, so the exit
// code and stderr are the ones a caller sees.
func TestMain(m *testing.M) {
	if os.Getenv("GM_RUN_MAIN") == "1" {
		os.Args = append([]string{"gm"}, strings.Fields(os.Getenv("GM_ARGS"))...)
		main()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestExpiredLoginIsAuthFailure(t *testing.T) {
	detail, auth := gwsFailure([]byte(expiredStdout), keyringStderr)
	if !auth {
		t.Fatalf("expired login not seen as a login failure: %q", detail)
	}
	if !strings.Contains(detail, "Token has been expired or revoked") {
		t.Fatalf("detail lost gws's own error text: %q", detail)
	}
}

func TestOtherAPIErrorKeepsItsText(t *testing.T) {
	detail, auth := gwsFailure([]byte(`{"error":{"code":404,"message":"Requested entity was not found.","reason":"notFound"}}`), keyringStderr)
	if auth || detail != "404 Requested entity was not found." {
		t.Fatalf("404 read as auth=%v detail=%q", auth, detail)
	}
	detail, auth = gwsFailure(nil, keyringStderr+"error: network unreachable\n")
	if auth || detail != "error: network unreachable" {
		t.Fatalf("stderr error read as auth=%v detail=%q", auth, detail)
	}
}

// The whole path: a stub gwsp answers like an expired login, and
// `gm --personal search` must exit 3 and name the real error and the fix.
func TestPersonalExpiredLoginEndToEnd(t *testing.T) {
	bin := t.TempDir()
	stub := "#!/bin/sh\nprintf '%s' '" + keyringStderr + "' >&2\nprintf '%s' '" + expiredStdout + "'\nexit 1\n"
	if err := os.WriteFile(filepath.Join(bin, "gwsp"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(),
		"GM_RUN_MAIN=1",
		"GM_ARGS=--personal search newer_than:1d",
		"PATH="+bin+":"+os.Getenv("PATH"),
		"HOME="+t.TempDir(),
	)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	err := cmd.Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != exitAuth {
		t.Fatalf("want exit %d, got %v; stderr: %s", exitAuth, err, stderr.String())
	}
	got := stderr.String()
	for _, want := range []string{"gm --personal: login expired", "Token has been expired or revoked", "`gwsp relogin`"} {
		if !strings.Contains(got, want) {
			t.Fatalf("stderr lacks %q: %s", want, got)
		}
	}
	if strings.Contains(got, "exit 1") || strings.Count(got, "\n") != 1 {
		t.Fatalf("want one clear line, got: %s", got)
	}
}
