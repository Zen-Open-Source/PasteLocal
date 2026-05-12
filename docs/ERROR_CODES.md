# Error Codes

> **This file is generated from `internal/errors/codes.go`. Do not hand-edit.**

All pastelocal errors follow the format `{"ok": false, "code": "CBxxxx",
"error": "...", "fix_hint": "..."}`. The first digit indicates the category:

- **CB1xxx** — Clipboard / image errors
- **CB2xxx** — Authentication errors
- **CB3xxx** — Protocol errors
- **CB4xxx** — Rate limiting errors

| Code | HTTP Status | Meaning | Fix Hint |
|------|------------|---------|----------|
| CB1001 | 400 Bad Request | No image on clipboard | Take a screenshot first |
| CB1002 | 500 Internal Server Error | Clipboard tool not installed | Run `pastelocal doctor --fix` |
| CB1003 | 500 Internal Server Error | Clipboard tool failed | Check `pastelocal logs` |
| CB1004 | 415 Unsupported Media Type | Image conversion failed | Save as PNG manually |
| CB1005 | 413 Payload Too Large | Image exceeds max_image_bytes | Raise limit in config |
| CB2001 | 401 Unauthorized | Invalid auth token | Re-run `pastelocal add-host <host>` to sync the token. |
| CB2002 | 401 Unauthorized | Missing auth token | Bug; report it |
| CB3001 | 426 Upgrade Required | Protocol version mismatch | Update local or remote binary |
| CB4001 | 429 Too Many Requests | Rate limit exceeded | Wait and retry |

## Remote Helper Exit Codes

The `pastelocal-remote` binary maps server error codes to exit codes:

| Exit Code | Meaning |
|-----------|---------|
| 0 | Success — image written, path printed to stdout |
| 1 | Tunnel not connected (connection refused) |
| 2 | Auth failure (CB2001 or CB2002) |
| 3 | No image on clipboard (CB1001) |
| 4 | Image format/conversion error (CB1004) |
| 5 | Image too large (CB1005) |
| 10 | Any other error |
