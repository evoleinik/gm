# gm — Gmail CLI

Convenient wrapper around `gws` for reading Gmail. Shells out to `gws` for OAuth/API, parses JSON responses in Go.

## Commands

```bash
gm                          # inbox: latest 10 messages
gm 5                        # inbox: latest N messages
gm read <id>                # read full email body (base64 decoded, HTML stripped)
gm search "query"           # search + display results
gm search "query" -n 20     # search with custom limit
gm reply <id> "message"     # reply to a thread
```

All commands support `--json` for machine-readable output.

## Build

```bash
go build -o gm .
cp gm ~/go/bin/gm
```

## Architecture

Single `main.go`, stdlib only. Calls `gws gmail users messages {list,get,send}` and parses responses.

## Exit codes

- 0: success
- 1: user error (bad args)
- 2: gws error (auth, network, etc.)

## Dependencies

- `gws` binary (at `/usr/local/bin/gws`) — handles OAuth, API calls
- Go 1.24+
