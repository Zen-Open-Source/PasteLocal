# PasteLocal Relay v1.0 — Multi-Device Encrypted Clipboard

The relay turns PasteLocal into a true multi-device clipboard bus. Your laptop, remote dev boxes, cloud VMs, and agents can share clipboard (text + screenshots) end-to-end encrypted without needing direct SSH tunnels between every pair.

## Quick Start (Laptop + 1 Remote)

1. On **laptop** (and every device):
   ```bash
   pastelocal relay init
   pastelocal relay pair https://your-relay.example.com   # or http://localhost:7332 for testing
   ```

2. Exchange fingerprints (shown by `pair` and `devices`).

3. On **both** sides add each other:
   ```bash
   pastelocal relay add-peer <other-fingerprint-or-device-id>
   ```

4. On laptop: take a screenshot or copy text.

5. On remote:
   ```bash
   pastelocal-remote --relay https://your-relay.example.com
   # prints path to the received file (auto-cleaned by skills)
   ```

Or inside an agent:
   - Grok: `/paste` (uses the packaged skill)
   - Claude: `/paste` (existing skill)

## Auto-Sync (Magic Path)

Edit `~/.config/pastelocal/config.toml`:

```toml
[relay]
enabled = true
relay_url = "https://..."
auto_upload = true   # push on every meaningful clipboard change
upload_ttl = 300
```

Run (or restart) `pastelocald`.

Now clipboard changes on the laptop appear on all paired remotes within a few seconds (watcher + per-peer E2EE upload).

## Sensitive / Concealed Clipboard Filtering (Password Manager Safety)

When `watch.enabled = true` together with the relay, PasteLocal can automatically
relay every clipboard change to your other devices. To prevent password managers
(1Password, Bitwarden, etc.) from having their secrets relayed, the daemon
respects the standard OS "concealed" signal:

* On **macOS**: items written with `org.nspasteboard.ConcealedType` (the same
  marker Raycast, Paste, and Maccy use) are **skipped entirely** by the watcher
  and never uploaded. The signature is absorbed so the item does not cause
  repeated processing.
* Explicit `GET /clipboard` (or `pastelocal-remote` without `--watch`) for a
  currently-concealed item returns error **CB1013** ("Content filtered as
  sensitive / concealed") with a clear fix hint. The secret bytes are never
  materialised in the daemon or sent over the wire.

```toml
[watch]
enabled = true

[watch.sensitive]
filter_concealed = true      # default
log_filtered_items = true    # shows in logs when something was filtered
```

The feature is **on by default** (safe) the moment you enable the watcher. Set
`filter_concealed = false` only if you have a very specific reason and accept
the risk. No secrets ever leave the machine unless you deliberately disable the
guard.

Detection failures (e.g. osascript problems) are logged (Info when
`log_filtered_items`, else Debug) with the full error+stderr; the item is
treated as non-concealed (fail-open) per the documented threat model. See
CB1013 for the explicit-read error case.

See also SECURITY.md for the threat-model rationale and ERROR_CODES.md (via the
registry in `internal/errors/codes.go`) for the exact CB1013 contract.

## Commands

- `pastelocal relay init` — create X25519 device keypair (0600)
- `pastelocal relay pair <url>` — register, get token, print fingerprint
- `pastelocal relay devices` — list everyone on the relay
- `pastelocal relay add-peer <id>` — enable E2E sharing with a peer (run on both)
- `pastelocal relay send <peer> [--file path]` — manual push (clipboard or file)
- `pastelocal relay inbox` — list what others have sent you
- `pastelocal relay fetch <sender>` — retrieve + decrypt one item (writes temp file)
- `pastelocal relay status` — health + peer count
- `pastelocal-remote --relay <url> --send /path --peer <id>` — send from remote to any peer
- `pastelocal-remote --relay <url>` — receive latest from any peer

## Running Your Own Relay Server

```bash
# In-memory (dev)
go run ./cmd/relay-server --port 7332

# With persistence (recommended)
go run ./cmd/relay-server --port 7332 --state-dir ~/.config/pastelocal/relay-state
# or install as systemd/launchd service (see docs)
```

The server never sees plaintext. All blobs are encrypted with per-pair X25519+HKDF+AES-GCM before they arrive.

## Grok Skills

The `skill/grok/` directory contains first-class Grok `SKILL.md` files:

```bash
cp -r skill/grok/paste ~/.grok/skills/pastelocal-paste
cp -r skill/grok/paste-send ~/.grok/skills/pastelocal-paste-send
```

Then just type `/paste` or `/paste-send` in any Grok session on a remote. The skills prefer the zero-config SSH tunnel when available and fall back to `--relay` automatically when needed.

A future `pastelocal relay grok-setup` will automate the copy + instructions.

## Security Notes (v1)

- Only the owner of a device can read its own inbox (token + server check).
- Rate limiting and TTL + compaction are enforced.
- Device private keys and relay tokens are 0600.
- Fingerprint (first 16 hex chars of device ID) is safe to share for manual verification.

See [SECURITY.md](./SECURITY.md) for full threat model.

## Status

Relay v1.0 completes the "Future Vision" section of the README. The core SSH path remains the recommended fast path; relay is the perfect fallback / multi-device / no-tunnel solution.

VisionPaste (Intelligent Screenshot Context) is a complementary local-first feature (v1): when the daemon's `[vision]` pipeline is configured, screenshots served over the direct (SSH) read path (and history fetches) include rich OCR + description text in the `ClipboardResponse` and as a `.analysis.txt` sidecar written by `pastelocal-remote`. 

**v1 scoping (intentional)**: Analysis is performed demand-driven at explicit read time on the serving daemon only (never inside the watcher goroutine or at push time, to keep the 2s poll fast). Relay carries raw bytes only. History responses carry Analysis on specific-entry fetches via on-demand re-analysis (storage remains raw bytes only; re-analysis happens if the screenshot is still on the source clipboard at fetch time). This resolves the plan's open question on watcher aggressiveness while delivering the highest-value path for agents. See README Features for config + full limitations, and the paste skills for agent UX.

See README and the Grok/Claude paste skills for usage.

Bugs / feedback: open an issue or PR.
