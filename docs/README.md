# pastelocal

**Secure local-to-remote clipboard bridge for SSH workflows.**

When you SSH into a remote machine, your local clipboard is unreachable. You
take a screenshot, switch to the remote shell, and realize you can't paste it.
pastelocal bridges that gap: a tiny daemon on your laptop serves clipboard
images over loopback, and an SSH `RemoteForward` tunnel carries them to the
remote host where `pastelocal-remote` writes them to disk — ready for any
tool (Claude, GPT, etc.) to read.

---

## 30-Second Install

### Standard SSH

```bash
# 1. Install and start the daemon
pastelocal init

# 2. Add a remote host (auto-edits ~/.ssh/config)
pastelocal add-host myserver

# 3. On the remote host, run the skill
pastelocal-remote   # prints a file path on success
```

### Termius

```bash
# 1. Install and start the daemon
pastelocal init

# 2. Print the values you need to paste into Termius
pastelocal add-host myserver --termius

# 3. Manually configure port forwarding in Termius (see TERMIUS.md)

# 4. Finish the installation
pastelocal add-host myserver --finish
```

---

## Termius Setup

If you use [Termius](https://termius.com) instead of OpenSSH, pastelocal
cannot auto-edit your SSH config. Instead, you configure a Remote port
forward manually inside Termius.

| Termius Field | Value |
|---|---|
| **Type** | Remote |
| **Local Address** | `127.0.0.1` |
| **Local Port** | `7331` (or your configured port) |
| **Remote Address** | `127.0.0.1` |
| **Remote Port** | `7331` (or your configured port) |

1. Open Termius → **Hosts** → select your host → **Edit** → **Advanced** →
   **Port Forwarding**
2. Add a new entry with the values above.
3. Run `pastelocal add-host <alias> --finish` to copy the binary, token, and
   skill to the remote host.

See [TERMIUS.md](TERMIUS.md) for detailed step-by-step instructions with
screenshots.

---

## Architecture

```
┌─────────────────────────────────────┐
│           LOCAL MACHINE              │
│                                     │
│  ┌──────────┐    ┌───────────────┐  │
│  │ OS       │    │ pastelocald   │  │
│  │ clipboard│◄───│ :7331 (loop) │  │
│  └──────────┘    └───────┬───────┘  │
│                          │          │
│          SSH RemoteForward tunnel   │
│          (127.0.0.1:7331)          │
└──────────────────────────┼──────────┘
                           │
┌──────────────────────────┼──────────┐
│        REMOTE HOST        │          │
│                          ▼          │
│                  ┌───────────────┐  │
│                  │ SSH listener  │  │
│                  │ :7331         │  │
│                  └───────┬───────┘  │
│                          │          │
│                  ┌───────▼───────┐  │
│                  │ pastelocal-   │  │
│                  │ remote        │  │
│                  └───────┬───────┘  │
│                          │          │
│                  ┌───────▼───────┐  │
│                  │ ~/.cache/     │  │
│                  │ pastelocal/   │  │
│                  │ <image>.png   │  │
│                  └───────────────┘  │
│                                     │
│  ┌───────────────────────────────┐  │
│  │ Claude skill: /paste          │  │
│  │ (~/.claude/commands/paste.md) │  │
│  └───────────────────────────────┘  │
└─────────────────────────────────────┘
```

**Flow:** `pastelocal-remote` → `GET /clipboard` (via SSH tunnel) →
`pastelocald` reads OS clipboard → returns base64 PNG →
`pastelocal-remote` decodes and writes to `~/.cache/pastelocal/` →
prints file path → skill reads and attaches the image.

---

## Troubleshooting (Top 10)

| # | Symptom | Error Code | Fix |
|---|---------|-----------|-----|
| 1 | "connection refused" on remote | — | SSH tunnel not established. Reconnect SSH or check `RemoteForward` in `~/.ssh/config`. |
| 2 | "Invalid auth token" | CB2001 | Token mismatch between local and remote. Run `pastelocal add-host <host> --update-token-only`. |
| 3 | "No image on clipboard" | CB1001 | Take a screenshot first, then retry. |
| 4 | "Clipboard tool not installed" | CB1002 | macOS: `brew install pngpaste`. Linux: install `wl-clipboard` or `xclip`. |
| 5 | "Protocol version mismatch" | CB3001 | Local and remote binaries are from different releases. Update both to the same version. |
| 6 | "Image exceeds max_image_bytes" | CB1005 | Raise `max_image_bytes` in `~/.config/pastelocal/config.toml`. |
| 7 | "Rate limit exceeded" | CB4001 | Wait and retry. Default: 60 req/min. Raise `rate_limit_per_minute` in config. |
| 8 | "Image conversion failed" | CB1004 | The clipboard contains a non-PNG image. Save as PNG manually. |
| 9 | "Missing auth token" | CB2002 | Internal bug — report it. |
| 10 | `pastelocal doctor` shows failures | — | Run `pastelocal doctor --fix` to auto-fix where safe. |

Full error reference: [ERROR_CODES.md](ERROR_CODES.md)

---

## Uninstall

```bash
# Remove everything (daemon, service unit, token, config)
pastelocal uninstall

# Remove but keep config files
pastelocal uninstall --keep-config

# Remove a single host
pastelocal remove-host myserver
```

Uninstall will:
1. Stop the daemon.
2. Remove the launchd/systemd service unit.
3. Delete the token from the keychain and file storage.
4. Remove `~/.config/pastelocal/` (unless `--keep-config`).
5. Remove the `RemoteForward` line from `~/.ssh/config`.

---

## Comparison

| Feature | **pastelocal** | **clipssh** | **cc-clip** | **claude-ssh-image-skill** |
|---|---|---|---|---|
| Clipboard → remote via SSH tunnel | ✅ | ✅ | ✅ | ✅ |
| Auth token (constant-time compare) | ✅ | ❌ | ❌ | ❌ |
| Loopback-only binding | ✅ | ❌ | ❌ | ❌ |
| Rate limiting | ✅ | ❌ | ❌ | ❌ |
| Max image size enforcement | ✅ | ❌ | ❌ | ❌ |
| Audit logging | ✅ | ❌ | ❌ | ❌ |
| Non-loopback connection rejection | ✅ | ❌ | ❌ | ❌ |
| Auto SSH config edit | ✅ | ❌ | ❌ | ❌ |
| Termius support | ✅ | ❌ | ❌ | ❌ |
| Token rotation command | ✅ | ❌ | ❌ | ❌ |
| Diagnostic doctor command | ✅ | ❌ | ❌ | ❌ |
| macOS Keychain / Linux libsecret | ✅ | ❌ | ❌ | ❌ |
| launchd / systemd service | ✅ | ❌ | ❌ | ❌ |
| Claude skill integration | ✅ | ❌ | ✅ | ✅ |
| Token never in argv/environ | ✅ | ❌ | ❌ | ❌ |
| Protocol version negotiation | ✅ | ❌ | ❌ | ❌ |

**pastelocal** is the only option that treats the SSH-tunneled clipboard
bridge as a security-sensitive service: loopback binding, bearer tokens with
constant-time comparison, rate limiting, size limits, and audit logging all
come standard. The alternatives expose your clipboard over the tunnel with no
authentication or access control.
