package main

import (
	"strings"
	"testing"
)

func resetAccount() {
	personal = false
	gwsBin = "gws"
	mailboxName = ""
}

// The default must stay the work account, so plain `gm` never changes meaning.
func TestDefaultIsWorkAccount(t *testing.T) {
	resetAccount()
	rest, jsonOut := parseGlobalFlags([]string{"search", "x"})
	if personal || gwsBin != "gws" || jsonOut {
		t.Fatalf("plain flags switched account: personal=%v bin=%s json=%v", personal, gwsBin, jsonOut)
	}
	if strings.Join(rest, " ") != "search x" {
		t.Fatalf("args changed: %q", rest)
	}
	if defaultBCC() != "evgeny@airshelf.ai" || gmCmd() != "gm" {
		t.Fatalf("work defaults wrong: bcc=%q cmd=%q", defaultBCC(), gmCmd())
	}
}

// --personal may sit anywhere, and it must move every part of gm to gwsp.
func TestPersonalSwitchesEverything(t *testing.T) {
	defer resetAccount()
	for _, args := range [][]string{
		{"--personal", "search", "x"},
		{"search", "x", "--personal"},
		{"search", "--json", "x", "--personal"},
	} {
		resetAccount()
		rest, _ := parseGlobalFlags(args)
		if !personal || gwsBin != "gwsp" {
			t.Fatalf("%q: not personal (bin=%s)", args, gwsBin)
		}
		for _, a := range rest {
			if a == "--personal" {
				t.Fatalf("%q: flag left in args %q", args, rest)
			}
		}
		if defaultBCC() != "" {
			t.Fatalf("personal mail would be copied to %q", defaultBCC())
		}
		if gmCmd() != "gm --personal" {
			t.Fatalf("follow-up command %q would send from the work account", gmCmd())
		}
	}
}

// A search with no results must say which mailbox it read and name the other one.
func TestNoResultsNamesMailbox(t *testing.T) {
	defer resetAccount()
	resetAccount()
	msg := noResultsMsg("Kandelaki", "evgeny@airshelf.ai")
	if !strings.Contains(msg, "evgeny@airshelf.ai") || !strings.Contains(msg, "add --personal") {
		t.Fatalf("work message does not name the mailbox and the way out: %s", msg)
	}
	usePersonal()
	msg = noResultsMsg("Kandelaki", "me@gmail.com")
	if !strings.Contains(msg, "me@gmail.com") || !strings.Contains(msg, "drop --personal") {
		t.Fatalf("personal message does not name the mailbox and the way out: %s", msg)
	}
}

// The upload path reads ~/.config/gws, the work login. Under --personal it must
// refuse, or a large personal mail would be drafted in the work account.
func TestPersonalRefusesWorkUploadPath(t *testing.T) {
	defer resetAccount()
	usePersonal()
	_, _, err := loadOAuthCreds()
	if err == nil || !strings.Contains(err.Error(), "--personal") {
		t.Fatalf("personal large-mail path did not refuse: %v", err)
	}
}

// Review links must open the drafting account, not the browser's first account.
func TestGmailBaseUsesMailbox(t *testing.T) {
	if got := gmailBase("me@gmail.com"); got != "https://mail.google.com/mail/u/me@gmail.com/" {
		t.Fatalf("personal link: %s", got)
	}
	if got := gmailBase("the work account"); got != "https://mail.google.com/mail/u/0/" {
		t.Fatalf("fallback link: %s", got)
	}
}
