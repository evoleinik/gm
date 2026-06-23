# gm

Gmail CLI for humans and agents. Read, search, reply, and send formatted emails from the terminal.

```
gm                                              # inbox
gm read 19cfbc6564b511d2                        # read email
gm search "from:bob invoice"                    # search
gm reply 19cfbc6564b511d2 "Thanks!"             # plain text reply
gm send bob@x.com "Report" --md report.md       # formatted email
```

## Why

- **One binary, no config** — wraps [gws](https://github.com/nicholasgasior/gws) for OAuth, does everything else in Go
- **Agent-friendly** — `--json` output on every command, deterministic exit codes, no interactive prompts
- **Markdown emails** — renders via pandoc with clean inline CSS, no external templates
- **Threading** — `--reply` attaches to Gmail threads correctly (threadId + In-Reply-To + References)
- **Attachments** — `--attach` for any file type, repeatable

## Install

```bash
go install github.com/evoleinik/gm@latest
```

Or build from source:

```bash
git clone https://github.com/evoleinik/gm
cd gm
go build -o gm .
cp gm ~/go/bin/  # or anywhere on PATH
```

### Prerequisites

- [gws](https://github.com/nicholasgasior/gws) — Google Workspace CLI (handles OAuth)
- [pandoc](https://pandoc.org/) — only needed for `--md` (markdown emails)

## Commands

### Inbox

```bash
gm              # latest 10
gm 20           # latest 20
gm --json       # machine-readable
```

### Read

```bash
gm read <message-id>                  # human-readable (shows attachments)
gm read <id1> <id2> <id3>             # batch read (parallel fetch)
gm read <message-id> --json           # full metadata + body + attachments
gm read <message-id> --save           # read + save attachments to current dir
gm read <message-id> --save ~/dl      # save attachments to specific dir
```

### Search

```bash
gm search "query"              # Gmail search syntax
gm search "from:bob" -n 5      # limit results
gm search "has:attachment newer_than:7d"
gm search "query" --full           # body preview (500 chars) + attachments
```

### Reply

```bash
gm reply <message-id> "message body"    # plain text, threads correctly
```

### Send

```bash
# Plain text
gm send to@email "Subject" --body "Hello"

# Markdown (rendered to styled HTML)
gm send to@email "Subject" --md document.md

# Stdin
echo "**bold** and *italic*" | gm send to@email "Subject" --md -

# With attachments
gm send to@email "Report" --md report.md --attach data.csv --attach chart.png

# Threaded reply (attaches to existing Gmail thread)
gm send to@email "Re: Topic" --md reply.md --reply 19cfbc6564b511d2

# CC, BCC
gm send to@email "Subject" --body "Hi" --cc alice@x.com --bcc boss@x.com

# Disable default BCC
gm send to@email "Subject" --body "Hi" --no-bcc
```

`gm send` always saves a **draft** (never auto-sends). On success it prints the
draft id and message id to stdout so you can verify or chain (`gm send --draft <id>`
to send). With `--json` it emits a structured object:

```json
{"draftId":"r-123","messageId":"19ef...","threadId":"19ef...","to":"bob@x.com",
 "subject":"Report","url":"https://mail.google.com/...","attachments":[{"filename":"data.csv","size":2048}],
 "via":"gws","action":"drafted"}
```

Attachments of any size work. Small messages go through `gws`; messages too large
for a single command-line argument (Linux caps one argv string at 128 KB — a ~90 KB
attachment is enough) are uploaded directly to Gmail's media endpoint, reusing gws's
stored OAuth token. `via` reports which path was used.

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | User error (bad arguments) |
| 2 | gws error (auth, network, API) |

## Architecture

Single `main.go`, Go stdlib only (no dependencies). Most Gmail API calls go through `gws`. MIME message building, base64 encoding, multipart assembly, and HTML stripping are done in pure Go.

```
gm ──→ gws (OAuth + Gmail API)            # default path
 │
 ├──→ Gmail media upload (net/http)       # large drafts only; reuses gws's refresh token
 │
 └──→ pandoc (markdown → HTML, only for --md)
```

The large-message path reads gws's plaintext credentials from `$GWS_CONFIG_DIR`
(default `~/.config/gws/`), refreshes an access token, and POSTs the RFC822 message
to `…/upload/gmail/v1/users/me/drafts` as `message/rfc822` (with the OAuth client's
`project_id` as the `x-goog-user-project` quota project). It works only where those
credentials are stored in plaintext.

## License

MIT
