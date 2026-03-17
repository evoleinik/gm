# gm — Gmail CLI

Read, search, reply, and send formatted emails. Shells out to `gws` for OAuth/API, builds MIME in pure Go, uses pandoc for markdown rendering.

## Commands

```bash
gm                                # inbox: latest 10 messages
gm 5                              # inbox: latest N messages
gm read <id>                      # read full email (HTML stripped)
gm search "query"                 # search + display results
gm search "query" -n 20           # search with custom limit
gm reply <id> "message"           # plain text reply (threaded)
gm send <to> <subject> [opts]     # formatted email (see below)
```

### gm send options

```bash
--body "text"       # plain text body
--md file.md        # markdown body (pandoc → styled HTML)
--md -              # markdown from stdin
--attach file       # attach file (repeatable)
--cc addr           # CC recipients
--bcc addr          # BCC (default: evgeny@airshelf.ai)
--no-bcc            # disable default BCC
--reply msgid       # thread onto existing message
```

All commands support `--json` for machine-readable output.

## Build

```bash
go build -o gm .
cp gm ~/go/bin/gm
```

## Architecture

Single `main.go`, stdlib only. Calls `gws gmail users messages {list,get,send}` and parses responses. MIME building (multipart, base64, attachments) is pure Go. Markdown → HTML via pandoc.

## Exit codes

- 0: success
- 1: user error (bad args)
- 2: gws error (auth, network, etc.)

## Dependencies

- `gws` binary — handles OAuth, API calls
- `pandoc` — only for `--md` flag
- Go 1.24+
