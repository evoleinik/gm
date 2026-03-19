package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

var gTimeout = 30 * time.Second

const (
	exitOK      = 0
	exitUser    = 1
	exitGWS     = 2
	gwsBin      = "gws"
	defaultMax  = 10
	idLen        = 16
	fromTruncLen = 24
	subjTruncLen = 44
)

// color helpers — only emit ANSI if stdout is a tty
var isTTY bool

func init() {
	fi, err := os.Stdout.Stat()
	if err == nil {
		isTTY = fi.Mode()&os.ModeCharDevice != 0
	}
}

func c(code, s string) string {
	if !isTTY {
		return s
	}
	return code + s + "\033[0m"
}

func dim(s string) string    { return c("\033[2m", s) }
func bold(s string) string   { return c("\033[1m", s) }
func cyan(s string) string   { return c("\033[36m", s) }
func yellow(s string) string { return c("\033[33m", s) }
func green(s string) string  { return c("\033[32m", s) }

func main() {
	args := os.Args[1:]

	// parse global flags
	jsonOut := false
	filtered := args[:0]
	for i := 0; i < len(args); i++ {
		if args[i] == "--json" {
			jsonOut = true
		} else if args[i] == "--timeout" && i+1 < len(args) {
			if d, err := time.ParseDuration(args[i+1]); err == nil {
				gTimeout = d
			}
			i++
		} else {
			filtered = append(filtered, args[i])
		}
	}
	args = filtered

	if len(args) == 0 {
		os.Exit(cmdInbox(defaultMax, jsonOut))
	}

	if args[0] == "--usage" || args[0] == "usage" {
		os.Exit(cmdUsage())
	}

	// gm <number>
	if n, err := strconv.Atoi(args[0]); err == nil && n > 0 {
		os.Exit(cmdInbox(n, jsonOut))
	}

	switch args[0] {
	case "read":
		if len(args) < 2 {
			die("usage: gm read <message-id>")
		}
		os.Exit(cmdRead(args[1], jsonOut))
	case "search":
		if len(args) < 2 {
			die("usage: gm search \"query\" [-n count]")
		}
		query := args[1]
		count := defaultMax
		for i := 2; i < len(args); i++ {
			if args[i] == "-n" && i+1 < len(args) {
				if n, err := strconv.Atoi(args[i+1]); err == nil && n > 0 {
					count = n
				}
				i++
			}
		}
		os.Exit(cmdSearch(query, count, jsonOut))
	case "reply":
		if len(args) < 3 {
			die("usage: gm reply <message-id> \"message body\"")
		}
		os.Exit(cmdReply(args[1], args[2], jsonOut))
	case "send":
		// gm send <to> <subject> [options]
		// Options: --body "text", --md file.md, --attach file, --cc addr, --bcc addr, --reply msgid
		os.Exit(cmdSend(args[1:], jsonOut))
	case "help", "--help", "-h":
		printUsage()
		os.Exit(exitOK)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", args[0])
		printUsage()
		os.Exit(exitUser)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `gm — Gmail CLI (wraps gws)

Usage:
  gm                          inbox (latest 10)
  gm <N>                      inbox (latest N)
  gm read <id>                read full email
  gm search "query"           search messages
  gm search "query" -n 20     search with limit
  gm reply <id> "message"     reply to thread (plain text)
  gm send <to> <subject> [options]  draft email (review in Gmail, then send)
  gm send --draft <id>             send a saved draft
  gm send <to> <subject> --now     send immediately (skip draft)

Send options:
  --body "text"         plain text body
  --md file.md          markdown body (rendered to HTML via pandoc)
  --attach file         attach a file (repeatable)
  --cc addr             CC recipients
  --bcc addr            BCC recipients (default: evgeny@airshelf.ai)
  --no-bcc              disable default BCC
  --reply msgid         thread onto an existing message

Examples:
  gm send bob@x.com "Hello" --body "Hi Bob"
  gm send bob@x.com "Report" --md report.md --attach data.csv
  gm send bob@x.com "Re: Topic" --md reply.md --reply 19cfbc6564b511d2
  echo "**hi**" | gm send bob@x.com "Hello" --md -

Flags:
  --json              machine-readable JSON output
  --timeout <dur>     gws call timeout (default 30s)
  --usage             show usage stats (last 30 days)
`)
}

func die(msg string) {
	fmt.Fprintln(os.Stderr, msg)
	os.Exit(exitUser)
}

// --- gws interaction ---

func callGWS(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, gwsBin, args...)
	var stderrBuf strings.Builder
	cmd.Stderr = &stderrBuf
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			stderr := stderrBuf.String()
			// filter out the noise line
			stderr = filterStderr(stderr)
			if strings.Contains(stderr, "401") || strings.Contains(strings.ToLower(stderr), "auth") || strings.Contains(strings.ToLower(stderr), "token") {
				fmt.Fprintf(os.Stderr, "gws auth failed — run 'gws auth login' to re-authenticate\n")
			} else if stderr != "" {
				fmt.Fprintf(os.Stderr, "gws error: %s\n", strings.TrimSpace(stderr))
			} else {
				fmt.Fprintf(os.Stderr, "gws error: exit %d\n", exitErr.ExitCode())
			}
			return nil, fmt.Errorf("gws exit %d", exitErr.ExitCode())
		}
		if _, ok := err.(*exec.Error); ok {
			fmt.Fprintf(os.Stderr, "gws not installed — see https://github.com/nicholasgasior/gws\n")
			return nil, err
		}
		return nil, err
	}
	return out, nil
}

func filterStderr(s string) string {
	var lines []string
	for _, line := range strings.Split(s, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "Using keyring backend:") {
			continue
		}
		lines = append(lines, trimmed)
	}
	return strings.Join(lines, "\n")
}

func listMessages(query string, maxResults int) ([]messageEntry, error) {
	params := map[string]interface{}{
		"userId":     "me",
		"maxResults": maxResults,
	}
	if query != "" {
		params["q"] = query
	}
	pJSON, _ := json.Marshal(params)

	out, err := callGWS("gmail", "users", "messages", "list", "--params", string(pJSON))
	if err != nil {
		return nil, err
	}

	var resp struct {
		Messages []struct {
			ID       string `json:"id"`
			ThreadID string `json:"threadId"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, fmt.Errorf("parse list response: %w", err)
	}

	if len(resp.Messages) == 0 {
		return nil, nil
	}

	// Parallel metadata fetches
	type result struct {
		idx   int
		entry messageEntry
		err   error
	}
	results := make([]result, len(resp.Messages))
	var wg sync.WaitGroup
	for i, m := range resp.Messages {
		wg.Add(1)
		go func(i int, id string) {
			defer wg.Done()
			entry, err := getMetadata(id)
			results[i] = result{idx: i, entry: entry, err: err}
		}(i, m.ID)
	}
	wg.Wait()

	entries := make([]messageEntry, 0, len(resp.Messages))
	for _, r := range results {
		if r.err == nil {
			entries = append(entries, r.entry)
		}
	}
	return entries, nil
}

type messageEntry struct {
	ID       string `json:"id"`
	From     string `json:"from"`
	Subject  string `json:"subject"`
	Date     string `json:"date"`
	DateRaw  string `json:"date_raw"`
	Snippet  string `json:"snippet"`
	ThreadID string `json:"thread_id"`
	To       string `json:"to"`
}

func getMetadata(id string) (messageEntry, error) {
	params := map[string]interface{}{
		"userId": "me",
		"id":     id,
		"format": "metadata",
	}
	pJSON, _ := json.Marshal(params)

	out, err := callGWS("gmail", "users", "messages", "get", "--params", string(pJSON))
	if err != nil {
		return messageEntry{}, err
	}

	var resp struct {
		ID       string `json:"id"`
		ThreadID string `json:"threadId"`
		Snippet  string `json:"snippet"`
		Payload  struct {
			Headers []struct {
				Name  string `json:"name"`
				Value string `json:"value"`
			} `json:"headers"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return messageEntry{}, err
	}

	entry := messageEntry{
		ID:       resp.ID,
		Snippet:  resp.Snippet,
		ThreadID: resp.ThreadID,
	}
	for _, h := range resp.Payload.Headers {
		switch h.Name {
		case "Subject":
			entry.Subject = h.Value
		case "From":
			entry.From = h.Value
		case "To":
			entry.To = h.Value
		case "Date":
			entry.DateRaw = h.Value
			entry.Date = formatDate(h.Value)
		}
	}
	return entry, nil
}

func formatDate(raw string) string {
	// try common RFC 2822 formats
	formats := []string{
		time.RFC1123Z,
		time.RFC1123,
		"Mon, 2 Jan 2006 15:04:05 -0700",
		"Mon, 2 Jan 2006 15:04:05 +0000",
		"Mon, 2 Jan 2006 15:04:05 -0700 (MST)",
		"2 Jan 2006 15:04:05 -0700",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, raw); err == nil {
			now := time.Now()
			if t.Year() == now.Year() {
				return t.Local().Format("Jan 02 15:04")
			}
			return t.Local().Format("Jan 02 2006")
		}
	}
	// fallback: return first 12 chars
	if len(raw) > 12 {
		return raw[:12]
	}
	return raw
}

// --- commands ---

func cmdInbox(count int, jsonOut bool) int {
	start := time.Now()
	entries, err := listMessages("", count)
	if err != nil {
		return exitGWS
	}
	ms := time.Since(start).Milliseconds()
	logUsage("inbox", err == nil, ms, len(entries))
	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(entries)
		return exitOK
	}
	printTable(entries)
	return exitOK
}

func cmdSearch(query string, count int, jsonOut bool) int {
	start := time.Now()
	entries, err := listMessages(query, count)
	if err != nil {
		return exitGWS
	}
	ms := time.Since(start).Milliseconds()
	logUsage("search", err == nil, ms, len(entries))
	if len(entries) == 0 {
		fmt.Fprintf(os.Stderr, "no results for %q (searched inbox). Try broader terms.\n", query)
		return exitOK
	}
	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(entries)
		return exitOK
	}
	printTableWithSnippets(entries)
	return exitOK
}

func printTable(entries []messageEntry) {
	if len(entries) == 0 {
		fmt.Fprintln(os.Stderr, "no messages")
		return
	}

	// header
	fmt.Fprintf(os.Stdout, "  %s  %s  %s  %s\n",
		dim(pad("ID", idLen)),
		dim(pad("FROM", fromTruncLen)),
		dim(pad("SUBJECT", subjTruncLen)),
		dim("DATE"),
	)

	for _, e := range entries {
		id := pad(e.ID, idLen)
		from := trunc(formatFrom(e.From), fromTruncLen)
		subj := trunc(e.Subject, subjTruncLen)
		date := e.Date

		fmt.Fprintf(os.Stdout, "  %s  %s  %s  %s\n",
			cyan(id),
			yellow(pad(from, fromTruncLen)),
			bold(pad(subj, subjTruncLen)),
			dim(date),
		)
	}
}

func printTableWithSnippets(entries []messageEntry) {
	if len(entries) == 0 {
		fmt.Fprintln(os.Stderr, "no messages")
		return
	}

	for _, e := range entries {
		id := pad(e.ID, idLen)
		from := trunc(formatFrom(e.From), fromTruncLen)
		subj := trunc(e.Subject, subjTruncLen)
		date := e.Date

		fmt.Fprintf(os.Stdout, "  %s  %s  %s  %s\n",
			cyan(id),
			yellow(pad(from, fromTruncLen)),
			bold(pad(subj, subjTruncLen)),
			dim(date),
		)
		if e.Snippet != "" {
			snip := html.UnescapeString(e.Snippet)
			if len(snip) > 100 {
				snip = snip[:100] + "..."
			}
			fmt.Fprintf(os.Stdout, "  %s  %s\n", strings.Repeat(" ", idLen), dim(snip))
		}
	}
}

func formatFrom(raw string) string {
	// "Name <email>" → "Name <email>" (truncation handles the rest)
	// "email@domain" → keep as-is
	return raw
}

func encodeSubject(s string) string {
	// If ASCII-only, return as-is
	isASCII := true
	for i := 0; i < len(s); i++ {
		if s[i] > 127 {
			isASCII = false
			break
		}
	}
	if isASCII {
		return s
	}
	// RFC 2047 encoded-word: =?charset?encoding?encoded-text?=
	return "=?UTF-8?B?" + base64.StdEncoding.EncodeToString([]byte(s)) + "?="
}

func trunc(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max-2]) + ".."
}

func pad(s string, width int) string {
	n := utf8.RuneCountInString(s)
	if n >= width {
		return s
	}
	return s + strings.Repeat(" ", width-n)
}

// --- read command ---

func cmdRead(id string, jsonOut bool) int {
	start := time.Now()
	params := map[string]interface{}{
		"userId": "me",
		"id":     id,
		"format": "full",
	}
	pJSON, _ := json.Marshal(params)

	out, err := callGWS("gmail", "users", "messages", "get", "--params", string(pJSON))
	logUsage("read", err == nil, time.Since(start).Milliseconds(), 1)
	if err != nil {
		return exitGWS
	}

	var msg fullMessage
	if err := json.Unmarshal(out, &msg); err != nil {
		fmt.Fprintf(os.Stderr, "parse error: %v\n", err)
		return exitGWS
	}

	// extract headers
	headers := extractHeaders(msg.Payload.Headers)
	body := extractBody(msg.Payload)

	if jsonOut {
		result := map[string]string{
			"id":      msg.ID,
			"from":    headers["From"],
			"to":      headers["To"],
			"date":    headers["Date"],
			"subject": headers["Subject"],
			"body":    body,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(result)
		return exitOK
	}

	// print human-readable
	fmt.Printf("%s %s\n", dim("From:"), headers["From"])
	fmt.Printf("%s %s\n", dim("To:"), headers["To"])
	fmt.Printf("%s %s\n", dim("Date:"), headers["Date"])
	fmt.Printf("%s %s\n", dim("Subject:"), bold(headers["Subject"]))
	fmt.Println()
	fmt.Println(body)

	return exitOK
}

type fullMessage struct {
	ID      string  `json:"id"`
	Payload payload `json:"payload"`
}

type payload struct {
	MimeType string    `json:"mimeType"`
	Headers  []header  `json:"headers"`
	Body     body      `json:"body"`
	Parts    []payload `json:"parts"`
}

type header struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type body struct {
	Size int    `json:"size"`
	Data string `json:"data"`
}

func extractHeaders(headers []header) map[string]string {
	m := map[string]string{}
	for _, h := range headers {
		switch h.Name {
		case "From", "To", "Date", "Subject", "Cc", "Message-ID", "In-Reply-To", "References":
			m[h.Name] = h.Value
		}
	}
	return m
}

func extractBody(p payload) string {
	// single part, no children
	if len(p.Parts) == 0 {
		if p.Body.Data != "" {
			decoded := decodeBase64URL(p.Body.Data)
			if strings.Contains(p.MimeType, "html") {
				return stripHTML(decoded)
			}
			return decoded
		}
		return ""
	}

	// multipart: prefer text/plain, fall back to text/html
	var textPlain, textHTML string
	findParts(p, &textPlain, &textHTML)

	if textPlain != "" {
		return textPlain
	}
	if textHTML != "" {
		return stripHTML(textHTML)
	}
	return "(no readable body)"
}

func findParts(p payload, textPlain, textHTML *string) {
	if p.MimeType == "text/plain" && p.Body.Data != "" && *textPlain == "" {
		*textPlain = decodeBase64URL(p.Body.Data)
	}
	if p.MimeType == "text/html" && p.Body.Data != "" && *textHTML == "" {
		*textHTML = decodeBase64URL(p.Body.Data)
	}
	for _, part := range p.Parts {
		findParts(part, textPlain, textHTML)
	}
}

func decodeBase64URL(s string) string {
	// Gmail API uses URL-safe base64 without padding — add padding for Go's decoder
	s = strings.ReplaceAll(s, "-", "+")
	s = strings.ReplaceAll(s, "_", "/")
	switch len(s) % 4 {
	case 2:
		s += "=="
	case 3:
		s += "="
	}
	decoded, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return "(decode error: " + err.Error() + ")"
	}
	return string(decoded)
}

var (
	reTag        = regexp.MustCompile(`<[^>]*>`)
	reStyle      = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	reScript     = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	reWhitespace = regexp.MustCompile(`\n{3,}`)
	reSpaces     = regexp.MustCompile(`[ \t]{2,}`)
)

func stripHTML(s string) string {
	s = reScript.ReplaceAllString(s, "")
	s = reStyle.ReplaceAllString(s, "")

	// block elements → newlines
	for _, tag := range []string{"</p>", "</div>", "</tr>", "</li>", "<br>", "<br/>", "<br />"} {
		s = strings.ReplaceAll(s, tag, "\n")
		s = strings.ReplaceAll(s, strings.ToUpper(tag), "\n")
	}

	s = reTag.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	s = reSpaces.ReplaceAllString(s, " ")
	s = reWhitespace.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

// --- reply command ---

func cmdReply(id, body string, jsonOut bool) int {
	start := time.Now()
	// first get the original message metadata to build reply headers
	params := map[string]interface{}{
		"userId": "me",
		"id":     id,
		"format": "metadata",
	}
	pJSON, _ := json.Marshal(params)

	out, err := callGWS("gmail", "users", "messages", "get", "--params", string(pJSON))
	if err != nil {
		return exitGWS
	}

	var orig struct {
		ID       string `json:"id"`
		ThreadID string `json:"threadId"`
		Payload  struct {
			Headers []header `json:"headers"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(out, &orig); err != nil {
		fmt.Fprintf(os.Stderr, "parse error: %v\n", err)
		return exitGWS
	}

	headers := extractHeaders(orig.Payload.Headers)

	// build RFC 2822 reply
	replyTo := headers["From"]
	subject := headers["Subject"]
	if !strings.HasPrefix(strings.ToLower(subject), "re:") {
		subject = "Re: " + subject
	}
	messageID := headers["Message-ID"]
	references := headers["References"]
	if references != "" {
		references = references + " " + messageID
	} else {
		references = messageID
	}

	// find our sender address (To header of original, or fall back to "me")
	fromAddr := headers["To"]
	if fromAddr == "" {
		fromAddr = "me"
	}
	// if To has multiple addresses, pick the first one
	if idx := strings.Index(fromAddr, ","); idx > 0 {
		fromAddr = strings.TrimSpace(fromAddr[:idx])
	}

	raw := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nIn-Reply-To: %s\r\nReferences: %s\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n%s",
		fromAddr, replyTo, subject, messageID, references, body)

	rawB64 := base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString([]byte(raw))

	sendJSON := map[string]interface{}{
		"raw":      rawB64,
		"threadId": orig.ThreadID,
	}
	sendBody, _ := json.Marshal(sendJSON)

	sendParams := map[string]interface{}{
		"userId": "me",
	}
	sendParamsJSON, _ := json.Marshal(sendParams)

	sendOut, err := callGWS("gmail", "users", "messages", "send",
		"--params", string(sendParamsJSON),
		"--json", string(sendBody))
	logUsage("reply", err == nil, time.Since(start).Milliseconds(), 1)
	if err != nil {
		return exitGWS
	}

	if jsonOut {
		os.Stdout.Write(sendOut)
		fmt.Println()
		return exitOK
	}

	var sendResp struct {
		ID string `json:"id"`
	}
	json.Unmarshal(sendOut, &sendResp)
	fmt.Fprintf(os.Stderr, "%s reply sent (id: %s)\n", green("OK"), sendResp.ID)
	return exitOK
}

// --- send command (pure Go: MIME building + pandoc for markdown) ---

const emailCSS = `body{font-family:-apple-system,system-ui,sans-serif;max-width:680px;margin:0 auto;padding:24px;color:#000;line-height:1.7;font-size:15px}h1{font-size:1.3em;font-weight:600;border-bottom:1px solid #e8e8e8;padding-bottom:6px}h2{font-size:1.1em;font-weight:600;margin-top:28px}h3{font-size:1em;color:#000}ul,ol{padding-left:24px}li{margin:4px 0}table{border-collapse:collapse;width:100%;font-size:14px}th,td{border:1px solid #e0e0e0;padding:6px 12px;text-align:left}th{background:#fafafa;font-weight:600}code{background:#f3f4f5;padding:1px 5px;border-radius:3px;font-size:.88em;color:#555}blockquote{border-left:3px solid #ddd;margin:8px 0;padding:2px 14px;color:#666}a{color:#4a7ccc;text-decoration:none}`

func cmdSend(args []string, jsonOut bool) int {
	// gm send --draft <id>  →  send an existing draft
	if len(args) >= 2 && args[0] == "--draft" {
		return cmdSendDraft(args[1], jsonOut)
	}

	if len(args) < 2 {
		die("usage: gm send <to> <subject> [--body text | --md file] [--attach file] [--cc addr] [--bcc addr] [--no-bcc] [--reply msgid] [--now]")
	}

	to := args[0]
	subject := args[1]
	bodyText := ""
	mdFile := ""
	cc := ""
	bcc := "evgeny@airshelf.ai"
	replyMsgID := ""
	sendNow := false
	var attachments []string

	for i := 2; i < len(args); i++ {
		switch args[i] {
		case "--body":
			i++; if i < len(args) { bodyText = args[i] }
		case "--md":
			i++; if i < len(args) { mdFile = args[i] }
		case "--attach":
			i++; if i < len(args) { attachments = append(attachments, args[i]) }
		case "--cc":
			i++; if i < len(args) { cc = args[i] }
		case "--bcc":
			i++; if i < len(args) { bcc = args[i] }
		case "--no-bcc":
			bcc = ""
		case "--reply":
			i++; if i < len(args) { replyMsgID = args[i] }
		case "--now":
			sendNow = true
		default:
			fmt.Fprintf(os.Stderr, "unknown send option: %s\n", args[i])
			return exitUser
		}
	}

	if bodyText == "" && mdFile == "" {
		die("gm send: need --body or --md")
	}

	// Resolve threading info
	threadID := ""
	inReplyTo := ""
	references := ""
	if replyMsgID != "" {
		threadID, inReplyTo, references = resolveThread(replyMsgID)
	}

	// Get plain text and HTML body
	plainBody, htmlBody, err := buildBodies(bodyText, mdFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "build body: %v\n", err)
		return exitUser
	}

	// Build MIME message
	raw, err := buildMIME(to, subject, cc, bcc, inReplyTo, references, plainBody, htmlBody, attachments)
	if err != nil {
		fmt.Fprintf(os.Stderr, "build MIME: %v\n", err)
		return exitUser
	}

	rawB64 := base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(raw)

	if sendNow {
		return doSend(rawB64, threadID, to, cc, bcc, subject, attachments, jsonOut)
	}
	return doDraft(rawB64, threadID, to, cc, bcc, subject, attachments, jsonOut)
}

func doDraft(rawB64, threadID, to, cc, bcc, subject string, attachments []string, jsonOut bool) int {
	start := time.Now()

	draftPayload := map[string]interface{}{
		"message": map[string]interface{}{"raw": rawB64},
	}
	if threadID != "" {
		draftPayload["message"].(map[string]interface{})["threadId"] = threadID
	}
	draftJSON, _ := json.Marshal(draftPayload)
	draftParams, _ := json.Marshal(map[string]string{"userId": "me"})

	out, err := callGWS("gmail", "users", "drafts", "create",
		"--params", string(draftParams),
		"--json", string(draftJSON))

	ms := time.Since(start).Milliseconds()
	logUsage("draft", err == nil, ms, len(attachments))

	if err != nil {
		return exitGWS
	}

	var resp struct {
		ID      string `json:"id"`
		Message struct {
			ID       string `json:"id"`
			ThreadID string `json:"threadId"`
		} `json:"message"`
	}
	json.Unmarshal(out, &resp)

	gmailURL := "https://mail.google.com/mail/u/0/#drafts?compose=" + resp.Message.ID

	if jsonOut {
		result := map[string]interface{}{
			"draft_id":    resp.ID,
			"message_id":  resp.Message.ID,
			"to":          to,
			"subject":     subject,
			"url":         gmailURL,
			"attachments": len(attachments),
			"action":      "drafted",
		}
		enc := json.NewEncoder(os.Stdout)
		enc.Encode(result)
		return exitOK
	}

	summary := fmt.Sprintf("Draft saved for %s", to)
	if cc != "" { summary += fmt.Sprintf(" (cc: %s)", cc) }
	if bcc != "" { summary += fmt.Sprintf(" (bcc: %s)", bcc) }
	if len(attachments) > 0 { summary += fmt.Sprintf(" [%d attachment(s)]", len(attachments)) }
	fmt.Fprintf(os.Stderr, "%s %s: %s\n", yellow("DRAFT"), summary, subject)
	fmt.Fprintf(os.Stderr, "  Review: %s\n", gmailURL)
	fmt.Fprintf(os.Stderr, "  Send:   gm send --draft %s\n", resp.ID)
	return exitOK
}

func doSend(rawB64, threadID, to, cc, bcc, subject string, attachments []string, jsonOut bool) int {
	start := time.Now()

	sendPayload := map[string]interface{}{"raw": rawB64}
	if threadID != "" {
		sendPayload["threadId"] = threadID
	}
	sendJSON, _ := json.Marshal(sendPayload)
	sendParams, _ := json.Marshal(map[string]string{"userId": "me"})

	out, err := callGWS("gmail", "users", "messages", "send",
		"--params", string(sendParams),
		"--json", string(sendJSON))

	ms := time.Since(start).Milliseconds()
	logUsage("send", err == nil, ms, len(attachments))

	if err != nil {
		return exitGWS
	}

	var resp struct{ ID string `json:"id"` }
	json.Unmarshal(out, &resp)

	if jsonOut {
		result := map[string]interface{}{
			"id":          resp.ID,
			"to":          to,
			"subject":     subject,
			"threaded":    threadID != "",
			"attachments": len(attachments),
			"action":      "sent",
		}
		enc := json.NewEncoder(os.Stdout)
		enc.Encode(result)
		return exitOK
	}

	summary := fmt.Sprintf("Sent to %s", to)
	if cc != "" { summary += fmt.Sprintf(" (cc: %s)", cc) }
	if bcc != "" { summary += fmt.Sprintf(" (bcc: %s)", bcc) }
	if len(attachments) > 0 { summary += fmt.Sprintf(" [%d attachment(s)]", len(attachments)) }
	if threadID != "" { summary += " [threaded]" }
	fmt.Fprintf(os.Stderr, "%s %s: %s\n", green("OK"), summary, subject)
	return exitOK
}

func cmdSendDraft(draftID string, jsonOut bool) int {
	start := time.Now()

	sendParams, _ := json.Marshal(map[string]string{"userId": "me"})
	sendJSON, _ := json.Marshal(map[string]interface{}{
		"id": draftID,
	})

	out, err := callGWS("gmail", "users", "drafts", "send",
		"--params", string(sendParams),
		"--json", string(sendJSON))

	ms := time.Since(start).Milliseconds()
	logUsage("send-draft", err == nil, ms, 0)

	if err != nil {
		return exitGWS
	}

	var resp struct {
		ID       string `json:"id"`
		ThreadID string `json:"threadId"`
	}
	json.Unmarshal(out, &resp)

	if jsonOut {
		result := map[string]interface{}{
			"id":     resp.ID,
			"action": "sent",
		}
		enc := json.NewEncoder(os.Stdout)
		enc.Encode(result)
		return exitOK
	}

	fmt.Fprintf(os.Stderr, "%s Draft sent (id: %s)\n", green("OK"), resp.ID)
	return exitOK
}

func resolveThread(msgID string) (threadID, inReplyTo, references string) {
	params, _ := json.Marshal(map[string]interface{}{
		"userId": "me", "id": msgID, "format": "metadata",
	})
	out, err := callGWS("gmail", "users", "messages", "get", "--params", string(params))
	if err != nil {
		return
	}
	var orig struct {
		ThreadID string `json:"threadId"`
		Payload  struct{ Headers []header `json:"headers"` } `json:"payload"`
	}
	if json.Unmarshal(out, &orig) != nil {
		return
	}
	hdrs := extractHeaders(orig.Payload.Headers)
	threadID = orig.ThreadID
	inReplyTo = hdrs["Message-ID"]
	if refs := hdrs["References"]; refs != "" {
		references = refs + " " + inReplyTo
	} else {
		references = inReplyTo
	}
	return
}

func buildBodies(bodyText, mdFile string) (plain, htmlContent string, err error) {
	if mdFile != "" {
		// Read markdown source
		var mdBytes []byte
		if mdFile == "-" {
			mdBytes, err = io.ReadAll(os.Stdin)
		} else {
			mdBytes, err = os.ReadFile(mdFile)
		}
		if err != nil {
			return "", "", fmt.Errorf("read markdown: %w", err)
		}

		// Normalize AI-generated markdown: ensure blank line before lists
		mdBytes = []byte(normalizeMD(string(mdBytes)))

		// Write to temp file for pandoc (always, since we may have modified content)
		tmpFile := ""
		if true {
			f, _ := os.CreateTemp("", "gm-*.md")
			f.Write(mdBytes)
			f.Close()
			tmpFile = f.Name()
			defer os.Remove(tmpFile)
		}

		// pandoc → plain text
		cmd := exec.Command("pandoc", tmpFile, "--to", "plain")
		plainOut, err := cmd.Output()
		if err != nil {
			return "", "", fmt.Errorf("pandoc plain: %w", err)
		}
		plain = string(plainOut)

		// pandoc → HTML with inline CSS
		cmd = exec.Command("pandoc", tmpFile, "-f", "markdown", "-t", "html5", "--standalone",
			"-V", "header-includes=<style>"+emailCSS+"</style>")
		htmlOut, err := cmd.Output()
		if err != nil {
			return "", "", fmt.Errorf("pandoc html: %w", err)
		}
		htmlContent = string(htmlOut)
	} else {
		plain = bodyText
		htmlContent = "<html><body><pre>" + html.EscapeString(bodyText) + "</pre></body></html>"
	}
	return
}

// normalizeMD fixes common AI-generated markdown issues before pandoc:
// 1. Insert blank line before list items that follow a non-blank, non-list line
// 2. Insert blank line before headings that follow a non-blank line
// 3. Insert blank line after standalone bold lines (**text**) followed by non-blank text
//    (LLMs write "**Header**\nDescription" but pandoc merges them into one paragraph)
func normalizeMD(s string) string {
	lines := strings.Split(s, "\n")
	var out []string
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if i > 0 {
			prevTrimmed := strings.TrimSpace(lines[i-1])
			isList := strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") ||
				(len(trimmed) > 2 && trimmed[0] >= '0' && trimmed[0] <= '9' && strings.Contains(trimmed[:3], "."))
			isHeading := strings.HasPrefix(trimmed, "#")
			prevIsBlank := prevTrimmed == ""
			prevIsList := strings.HasPrefix(prevTrimmed, "- ") || strings.HasPrefix(prevTrimmed, "* ") ||
				(len(prevTrimmed) > 2 && prevTrimmed[0] >= '0' && prevTrimmed[0] <= '9' && strings.Contains(prevTrimmed[:3], "."))

			// Insert blank line before list/heading if previous line isn't blank/list
			if (isList && !prevIsBlank && !prevIsList) || (isHeading && !prevIsBlank) {
				out = append(out, "")
			}

			// Insert blank line after standalone bold line followed by non-blank text
			// Catches: "**Bold header**\nDescription" and "**Bold** (extra) — stuff\nDescription"
			if !prevIsBlank && trimmed != "" && !isHeading && !isList &&
				strings.HasPrefix(prevTrimmed, "**") && !strings.HasPrefix(trimmed, "**") {
				out = append(out, "")
			}
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func buildMIME(to, subject, cc, bcc, inReplyTo, references, plain, htmlContent string, attachments []string) ([]byte, error) {
	var buf strings.Builder
	boundary := fmt.Sprintf("==%x==", time.Now().UnixNano())

	// Headers
	buf.WriteString("From: me\r\n")
	buf.WriteString("To: " + to + "\r\n")
	if cc != "" { buf.WriteString("Cc: " + cc + "\r\n") }
	if bcc != "" { buf.WriteString("Bcc: " + bcc + "\r\n") }
	buf.WriteString("Subject: " + encodeSubject(subject) + "\r\n")
	if inReplyTo != "" { buf.WriteString("In-Reply-To: " + inReplyTo + "\r\n") }
	if references != "" { buf.WriteString("References: " + references + "\r\n") }
	buf.WriteString("MIME-Version: 1.0\r\n")

	if len(attachments) > 0 {
		mixedBoundary := boundary + "-mixed"
		altBoundary := boundary + "-alt"

		buf.WriteString("Content-Type: multipart/mixed; boundary=\"" + mixedBoundary + "\"\r\n\r\n")

		// Body part (multipart/alternative)
		buf.WriteString("--" + mixedBoundary + "\r\n")
		buf.WriteString("Content-Type: multipart/alternative; boundary=\"" + altBoundary + "\"\r\n\r\n")
		writeAlternativeParts(&buf, altBoundary, plain, htmlContent)

		// Attachment parts
		for _, path := range attachments {
			data, err := os.ReadFile(path)
			if err != nil {
				return nil, fmt.Errorf("read attachment %s: %w", path, err)
			}
			filename := filepath.Base(path)
			buf.WriteString("--" + mixedBoundary + "\r\n")
			buf.WriteString("Content-Type: application/octet-stream; name=\"" + filename + "\"\r\n")
			buf.WriteString("Content-Disposition: attachment; filename=\"" + filename + "\"\r\n")
			buf.WriteString("Content-Transfer-Encoding: base64\r\n\r\n")
			writeBase64(&buf, data)
		}
		buf.WriteString("--" + mixedBoundary + "--\r\n")
	} else {
		altBoundary := boundary + "-alt"
		buf.WriteString("Content-Type: multipart/alternative; boundary=\"" + altBoundary + "\"\r\n\r\n")
		writeAlternativeParts(&buf, altBoundary, plain, htmlContent)
	}

	return []byte(buf.String()), nil
}

func writeAlternativeParts(buf *strings.Builder, boundary, plain, htmlContent string) {
	buf.WriteString("--" + boundary + "\r\n")
	buf.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	buf.WriteString("Content-Transfer-Encoding: base64\r\n\r\n")
	writeBase64(buf, []byte(plain))

	buf.WriteString("--" + boundary + "\r\n")
	buf.WriteString("Content-Type: text/html; charset=utf-8\r\n")
	buf.WriteString("Content-Transfer-Encoding: base64\r\n\r\n")
	writeBase64(buf, []byte(htmlContent))

	buf.WriteString("--" + boundary + "--\r\n")
}

func writeBase64(buf *strings.Builder, data []byte) {
	encoded := base64.StdEncoding.EncodeToString(data)
	// Wrap at 76 chars per RFC 2045
	for i := 0; i < len(encoded); i += 76 {
		end := i + 76
		if end > len(encoded) {
			end = len(encoded)
		}
		buf.WriteString(encoded[i:end] + "\r\n")
	}
}

func cmdUsage() int {
	home, _ := os.UserHomeDir()
	f, err := os.Open(home + "/.gm/usage.jsonl")
	if err != nil {
		fmt.Fprintln(os.Stderr, "no usage data yet")
		return exitOK
	}
	defer f.Close()

	var total, ok int
	var totalMs int64
	cmds := map[string]int{}
	cutoff := time.Now().AddDate(0, 0, -30)

	scanner := json.NewDecoder(f)
	for {
		var entry map[string]interface{}
		if err := scanner.Decode(&entry); err != nil {
			break
		}
		ts, _ := time.Parse(time.RFC3339, fmt.Sprint(entry["ts"]))
		if ts.Before(cutoff) {
			continue
		}
		total++
		if b, _ := entry["ok"].(bool); b {
			ok++
		}
		if ms, _ := entry["ms"].(float64); ms > 0 {
			totalMs += int64(ms)
		}
		if cmd, _ := entry["cmd"].(string); cmd != "" {
			cmds[cmd]++
		}
	}

	if total == 0 {
		fmt.Fprintln(os.Stderr, "no usage in last 30 days")
		return exitOK
	}

	fmt.Fprintf(os.Stdout, "gm usage (30 days)\n")
	fmt.Fprintf(os.Stdout, "  calls: %d  success: %d/%d (%.0f%%)\n", total, ok, total, float64(ok)/float64(total)*100)
	fmt.Fprintf(os.Stdout, "  avg latency: %dms\n", totalMs/int64(total))
	fmt.Fprintf(os.Stdout, "  commands:")
	for cmd, n := range cmds {
		fmt.Fprintf(os.Stdout, "  %s=%d", cmd, n)
	}
	fmt.Fprintln(os.Stdout)
	return exitOK
}

// --- telemetry (AX Principle #10) ---

func logUsage(cmd string, ok bool, ms int64, resultCount int) {
	defer func() { recover() }() // never break the tool
	home, _ := os.UserHomeDir()
	dir := home + "/.gm"
	os.MkdirAll(dir, 0755)
	f, err := os.OpenFile(dir+"/usage.jsonl", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	entry := map[string]interface{}{
		"ts":      time.Now().Format(time.RFC3339),
		"cmd":     cmd,
		"ok":      ok,
		"ms":      ms,
		"results": resultCount,
	}
	line, _ := json.Marshal(entry)
	f.Write(line)
	f.WriteString("\n")
}
