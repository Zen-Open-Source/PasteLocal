# Architecture

## Components Overview

clipbridge has four components, each with a single responsibility:

### 1. Daemon (`clipbridged`)

A long-running HTTP server on the local machine. It binds to `127.0.0.1:7331`
(loopback only) and exposes three endpoints:

| Endpoint | Auth | Purpose |
|---|---|---|
| `GET /clipboard` | Bearer token | Reads the OS clipboard, returns base64-encoded PNG |
| `GET /health` | None | Returns `{"ok": true}` for liveness checks |
| `GET /version` | None | Returns protocol version and binary version |

The daemon enforces:
- **Concurrency:** `max_in_flight` semaphore (default 4)
- **Rate limit:** token-bucket at `rate_limit_per_minute` (default 60)
- **Size limit:** `max_image_bytes` (default 50 MiB)
- **Auth:** Bearer token with constant-time comparison
- **Loopback only:** `ConnContext` rejects any non-loopback connection
- **Audit:** optional JSON-lines audit log with image SHA-256 hash

Signal handling:
- `SIGHUP` — reload configuration and re-read token from store
- `SIGTERM` — graceful shutdown with 5-second drain timeout

### 2. Remote Helper (`clipbridge-remote`)

A one-shot CLI that runs on the remote host. It:
1. Reads the auth token from `~/.config/clipbridge/token` (mode 0600)
2. Checks protocol version via `GET /version`
3. Fetches clipboard image via `GET /clipboard` through the SSH tunnel
4. Base64-decodes the image and writes it to `~/.cache/clipbridge/` (mode 0600)
5. Prints the absolute file path to stdout

Exit codes: `0` = success, `1` = tunnel not connected, `2` = auth failure,
`3` = no image, `4` = format error, `5` = image too large, `10` = other error.

The token never appears in `argv` or `environ`; it is read from a file only.

### 3. Control CLI (`clipbridge`)

The user-facing command-line tool built with Cobra. Subcommands:

| Command | Group | Purpose |
|---|---|---|
| `init` | Daemon | Generate token, write config, install service, start daemon |
| `start` / `stop` / `restart` | Daemon | Service lifecycle |
| `rotate-token` | Daemon | Generate new token, print hosts that need update |
| `uninstall` | Daemon | Stop, remove service, delete token and config |
| `add-host <alias>` | Host | Install binary + token + skill on remote, edit SSH config |
| `remove-host <alias>` | Host | Uninstall from remote, remove SSH config entry |
| `list-hosts` | Host | Show configured hosts (table or JSON) |
| `status` | Diagnostic | Daemon status, port, uptime, host reachability |
| `doctor` | Diagnostic | Run all diagnostic checks (`--fix` to auto-fix) |
| `logs` | Diagnostic | Tail daemon logs (launchd log or journalctl) |

### 4. Skill (`paste.md`)

A Claude skill installed at `~/.claude/commands/paste.md` on the remote host.
It instructs Claude to:
1. Run `clipbridge-remote` via Bash
2. Read the resulting image file
3. Delete the file
4. Confirm to the user: "Got it, image attached."

---

## Trust Boundaries

```
┌───────────────────────────────────────────────────────────┐
│                     TRUST BOUNDARY 1                      │
│          Local Machine (fully trusted)                    │
│                                                          │
│  ┌─────────┐    ┌──────────┐    ┌──────────────────┐    │
│  │  User   │───▶│ clipbrid │    │  OS Keychain /   │    │
│  │         │    │  ged     │◀──▶│  token file      │    │
│  └─────────┘    └────┬─────┘    └──────────────────┘    │
│                      │                                    │
│               127.0.0.1:7331                              │
│                      │                                    │
└──────────────────────┼────────────────────────────────────┘
                       │ SSH RemoteForward tunnel
                       │ (encrypted by SSH)
┌──────────────────────┼────────────────────────────────────┐
│                     │                                     │
│            TRUST BOUNDARY 2                                │
│          Remote Host (partially trusted)                  │
│                      │                                    │
│               127.0.0.1:7331                              │
│                      │                                    │
│                ┌─────▼──────┐    ┌──────────────────┐    │
│                │ clipbridge- │    │  token file      │    │
│                │ remote     │◀──▶│  (mode 0600)     │    │
│                └─────┬──────┘    └──────────────────┘    │
│                      │                                    │
│                ┌─────▼──────┐                             │
│                │ ~/.cache/  │                             │
│                │ clipbridge/│                             │
│                └────────────┘                             │
│                                                          │
│  ┌──────────────────────────────────────┐                │
│  │ Claude / other tools (trust level:   │                │
│  │ you chose to run them)              │                │
│  └──────────────────────────────────────┘                │
└───────────────────────────────────────────────────────────┘
```

**Trust boundary 1:** The local machine is fully trusted. The daemon binds to
loopback only, so no network-facing attack surface exists. The token is stored
in the OS keychain with a file fallback.

**Trust boundary 2:** The remote host is partially trusted. We trust the host
admin (you) but assume other users or processes on the remote host are
untrusted. The token file is mode 0600 (owner-only), and the cached images
are also mode 0600.

**Between boundaries:** The SSH tunnel provides encryption and integrity. The
tunnel is established by the user's SSH client, not by clipbridge.

---

## Wire Protocol

Protocol version: **1**

All responses are JSON. Successful responses include `"ok": true`. Error
responses include `"ok": false` plus `code`, `error`, and `fix_hint`.

### `GET /clipboard`

**Request:**
```
GET /clipboard HTTP/1.1
Host: 127.0.0.1:7331
Authorization: Bearer <token>
```

**Success response (200):**
```json
{
  "ok": true,
  "image": "<base64-encoded PNG>",
  "format": "png",
  "byte_count": 123456,
  "captured_at": "2025-01-15T10:30:00Z"
}
```

**Error response (4xx/5xx):**
```json
{
  "ok": false,
  "code": "CB2001",
  "error": "Invalid auth token",
  "fix_hint": "Re-run `clipbridge add-host <host>` to sync the token."
}
```

### `GET /health`

**Success response (200):**
```json
{"ok": true}
```

### `GET /version`

**Success response (200):**
```json
{
  "ok": true,
  "protocol_version": 1,
  "binary_version": "0.1.0"
}
```

---

## Data Flow: `/paste` Operation

```
User types /paste in Claude on remote host
         │
         ▼
Claude reads ~/.claude/commands/paste.md
         │
         ▼
Claude runs: clipbridge-remote
         │
         ▼
clipbridge-remote reads token from ~/.config/clipbridge/token
         │
         ▼
clipbridge-remote → GET /version → clipbridged (via SSH tunnel)
         │  (check protocol compatibility)
         ▼
clipbridge-remote → GET /clipboard → clipbridged (via SSH tunnel)
         │
         ├─ clipbridged: acquire semaphore slot
         ├─ clipbridged: check rate limit
         ├─ clipbridged: validate Bearer token (constant-time compare)
         ├─ clipbridged: lock mutex, read OS clipboard via platform tool
         ├─ clipbridged: check image size ≤ max_image_bytes
         ├─ clipbridged: base64-encode PNG
         ├─ clipbridged: write audit log (if configured)
         └─ clipbridged: release mutex and semaphore
         │
         ▼
clipbridge-remote receives JSON response
         │
         ├─ base64-decode image
         ├─ write to ~/.cache/clipbridge/clipbridge-<ts>-<rand6>.png (0600)
         └─ print absolute path to stdout
         │
         ▼
Claude reads the image file via Read tool
         │
         ▼
Claude deletes the file via rm
         │
         ▼
Claude confirms: "Got it, image attached."
```

---

## Configuration System

**File format:** TOML  
**Default path:** `~/.config/clipbridge/config.toml`  
**Override:** `--config` flag or `CLIPBRIDGE_CONFIG_DIR` env var  
**Env overrides:** `CLIPBRIDGE_PORT`, `CLIPBRIDGE_LOG_LEVEL`

### Default values

| Key | Default | Description |
|---|---|---|
| `port` | `7331` | Loopback listen port |
| `transport` | `"tcp"` | Transport protocol (currently TCP only) |
| `log_level` | `"info"` | Log level: debug, info, warn, error |
| `max_image_bytes` | `52428800` (50 MiB) | Maximum clipboard image size |
| `max_in_flight` | `4` | Max concurrent clipboard reads |
| `rate_limit_per_minute` | `60` | Token-bucket rate limit |
| `audit_log` | `""` | Path to JSON-lines audit log (empty = disabled) |
| `macos.pngpaste_path` | `""` | Override pngpaste binary path |
| `linux.clipboard_tool` | `"auto"` | Clipboard tool: auto, wl-paste, xclip |

### Host entries

Each host is stored as a nested table:

```toml
[hosts.myserver]
added_at = 2025-01-15T10:30:00Z
remote_port = 7331
remote_user = "deploy"
remote_path = "~/.local/bin/clipbridge-remote"
termius = false
```

Config is saved atomically (write to temp file, fsync, rename). SIGHUP
triggers a live reload without restart.

---

## Service Management

### macOS (launchd)

- **Plist path:** `~/Library/LaunchAgents/com.clipbridge.daemon.plist`
- **Label:** `com.clipbridge.daemon`
- **RunAtLoad:** true
- **KeepAlive:** true
- **Stdout log:** `~/Library/Logs/clipbridge.log`
- **Stderr log:** `~/Library/Logs/clipbridge.err`
- **Commands:** `launchctl load/unload`, `launchctl print` for PID

### Linux (systemd)

- **Unit path:** `~/.config/systemd/user/clipbridge.service`
- **Type:** `simple`
- **Restart:** `on-failure` (10s delay)
- **WantedBy:** `default.target`
- **Commands:** `systemctl --user start/stop/restart/enable/status`
- **Logs:** `journalctl --user -u clipbridge.service`

Both service types are installed and managed by `clipbridge init`,
`clipbridge start`, `clipbridge stop`, and `clipbridge uninstall`.
