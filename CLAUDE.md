# gm — Gmail CLI

Read, search, reply, and send formatted emails. Shells out to `gws` for OAuth/API, builds MIME in pure Go, uses pandoc for markdown rendering.

## Commands

```bash
gm                                # inbox: latest 10 messages
gm 5                              # inbox: latest N messages
gm read <id>                      # read full email (shows attachments)
gm read <id> <id2> ...            # batch read (parallel fetch)
gm read <id> --save [dir]         # read + save attachments to dir (default: .)
gm search "query"                 # search + display results
gm search "query" -n 20           # search with custom limit
gm search "query" --full          # search with body preview (500 chars) + attachments
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

`gm send` always saves a DRAFT (never auto-sends). On success it prints the draft id
+ message id to stdout; `--json` emits `{"draftId","messageId","threadId","to","subject","url","attachments":[{"filename","size"}],"via","action"}`.

## Build

```bash
go build -o gm .
cp gm ~/bin/gm   # PATH location on box (also ~/go/bin works)
```

## Architecture

Single `main.go`, stdlib only. Default path calls `gws gmail users messages/drafts {list,get,create,send}` and parses responses. MIME building (multipart, base64, attachments) is pure Go. Markdown → HTML via pandoc.

**Large-attachment path (the `--attach` fix):** Gmail's draft body is passed to gws as a single `--json` argv string, but Linux caps one argv string at `MAX_ARG_STRLEN` (128 KB) — so a draft with a ~90 KB+ attachment makes `exec` fail with E2BIG (previously a silent exit 2). When the encoded body would exceed `maxArgvBody` (120 KB), `createDraft` escalates to `createDraftViaUpload`: it reads gws's plaintext `credentials.json` (`$GWS_CONFIG_DIR`, default `~/.config/gws/`), refreshes an access token, and POSTs the RFC822 message to `…/upload/gmail/v1/users/me/drafts?uploadType=multipart` as a `message/rfc822` media part. The OAuth client's own project has Gmail disabled by default, so the request sets `x-goog-user-project` to the `project_id` from `client_secret.json` (the quota project that has Gmail enabled). This path only works where credentials are plaintext (box, not the encrypted-cred machines). gws's own `--upload` is unusable here because it hardcodes `application/octet-stream`, which Gmail rejects for drafts.

## Exit codes

- 0: success
- 1: user error (bad args)
- 2: gws error (auth, network, etc.)

## Dependencies

- `gws` binary — handles OAuth, API calls
- `pandoc` — only for `--md` flag
- Go 1.24+
