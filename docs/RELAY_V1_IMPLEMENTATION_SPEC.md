# PasteLocal Relay v1.0 — Production Multi-Device Encrypted Clipboard Sync
**Implementation Specification for Autonomous /implement Agent**

**Prepared by:** Grok (xAI) — overnight session prep for user  
**Date:** 2026-05-17  
**Target:** `PasteLocal` codebase (current `dev` binaries)  
**Recommended invocation:**  
`/implement --effort 4 "Implement Relay v1.0 per the full spec in docs/RELAY_V1_IMPLEMENTATION_SPEC.md. Treat the entire spec as the authoritative plan. Focus on making end-to-end relay + auto-sync + Grok skills work reliably."`

**Effort guidance for orchestrator:** Use 4 (strong coverage: multiple generals + security + tests specialists). Security is critical (crypto, auth tokens, inbox access). Tests are essential (new persistence, encryption paths, watcher integration).

---

## 1. Executive Summary & Motivation

PasteLocal's core SSH-tunnel workflow is solid and production-ready. The **experimental relay** (E2E encrypted multi-device clipboard sharing without requiring direct SSH tunnels between every pair of machines) is the #1 incomplete area explicitly called out in:

- [README.md](/Users/MorgansM4/Documents/Coding/PasteLocal/README.md) (lines 152-168, 232, 238-244 "Future Vision")
- [CONTRIBUTING.md](/Users/MorgansM4/Documents/Coding/PasteLocal/CONTRIBUTING.md) (lines 91-93)
- [docs/ARCHITECTURE.md](/Users/MorgansM4/Documents/Coding/PasteLocal/docs/ARCHITECTURE.md) (lines 70-85)
- Config defaults and handler comments

**Current state (gaps confirmed by code inspection):**
- CLI setup commands (`pastelocal relay init/pair/add-peer/devices`) exist and are mostly complete (cmd/pastelocal/main.go:1272-1476).
- `internal/relay/client.go` has excellent `EncryptAndUpload`, `DownloadAndDecrypt`, inbox list/fetch helpers using proper X25519 + HKDF + AES-GCM (via `internal/relay/envelope.go` and `internal/crypto/crypto.go`).
- `cmd/relay-server/main.go` (single-file, ~630 lines) has inbox support (`/api/v1/upload/{receiver}`, `/api/v1/inbox*`) and legacy paths, but is **100% in-memory** (no persistence across restarts), has duplicated handler code at the bottom, and some formatting/organization debt.
- Daemon integration is **placeholder-only**: [internal/server/handlers.go:307-319](/Users/MorgansM4/Documents/Coding/PasteLocal/internal/server/handlers.go) uses `relayKeyPair.PrivateKey.Bytes()` as secret (self-encryption, useless for peers) and only triggers on read with `AutoUpload`.
- No control-plane CLI verbs for `relay send`, `relay inbox`, `relay fetch`.
- `pastelocal-remote` has `runRelayFetch` but no symmetric `runRelaySend` (remote → any paired peer via relay).
- Watcher ([internal/server/watcher.go](/Users/MorgansM4/Documents/Coding/PasteLocal/internal/server/watcher.go) + server.go:287) is excellent (debounced, throttled, WS notifications) but never pushes to relay.
- No Grok-native skills (existing `skill/*.md` are Claude `/command` style only).
- No doctor checks, TUI relay status, or end-to-end happy-path tests for relay send/receive.

**The vision (from README):** Make clipboard sharing feel native across laptop + any number of remotes/agents, even when direct SSH isn't possible (cloud VMs, mobile, friends' machines, etc.). The relay becomes the "clipboard bus."

**Success for this spec:** After implementation, a user can:
1. Run a local `relay-server` (or use a hosted one).
2. `pastelocal relay init && pair` on laptop and on a remote dev box.
3. `pastelocal relay add-peer <fingerprint>` (or device ID) on both sides.
4. On laptop: copy screenshot → it auto-appears in the remote's `pastelocal-remote --relay --list` or via new Grok/Claude skill.
5. On remote: `pastelocal-remote --send some-file --relay <peer>` pushes it back to laptop clipboard.
6. Restart the relay-server; pending inbox items survive.
7. Use `/paste-relay` (or equivalent) inside Grok on the remote and it just works.

**Non-goals for v1 (explicitly defer):**
- Full peer-to-peer gossip (still hub-and-spoke via relay server is fine).
- Mobile apps or browser extension.
- End-to-end compression or delta sync.
- Relay clustering / high availability.
- Rich content types beyond png/text (already supported in core).

---

## 2. Target User Stories & Concrete Command Sequences

**Story A — Laptop + 1 Remote (SSH + Relay fallback)**
```bash
# Laptop
pastelocal init
pastelocal relay init
pastelocal relay pair https://relay.example.com   # or localhost:7332 for testing
# ... copy a screenshot ...

# On remote (after `pastelocal add-host ...` or manual)
pastelocal-remote --relay https://relay.example.com --list
pastelocal-remote --relay https://relay.example.com --fetch <sender>
# or via skill: /paste-relay
```

**Story B — Auto-sync on change (the magic)**
- Enable `watch.enabled = true` + `relay.auto_upload = true` + at least one peer.
- Change clipboard locally (screenshot or substantial text) → within ~3s the watcher detects it, properly encrypts for each paired peer, uploads to their inbox.
- Remote sees it via `--list` or gets a WS/poll notification.

**Story C — Pure relay (no SSH tunnel at all)**
Two cloud boxes + laptop all paired; any can push to any other via the relay.

**Story D — Grok user experience (new)**
User on remote has Grok running. They run a one-time install or the skill is auto-discoverable. They type `/paste` or `/paste-relay "from my laptop"` and Grok executes the right `pastelocal-remote` + Read + rm dance, exactly like the Claude skills but using Grok's tool-calling + SKILL.md contract.

---

## 3. Architecture Decisions (to be followed)

1. **Persistence for relay-server (v1)**: Simple, dependency-free, atomic file-based store.
   - One `state.json` (or directory of per-device inboxes for easy cleanup).
   - On every mutation: write to `state.json.tmp`, `fsync`, rename.
   - Background compaction goroutine (every 5 min or on size threshold) that rewrites without expired blobs.
   - On startup: load + validate + drop expired.
   - Bonus: optional `--persist-dir` flag; default `~/.config/pastelocal/relay-state/` or in-memory if flag omitted (for dev).
   - Rationale: keeps `go.mod` clean (no new deps for v1). If we want SQLite later, it's a drop-in behind an interface.

2. **Encryption model (keep existing, fix usage)**:
   - For inbox delivery: sender uses `EncryptAndUpload(peerPub, ...)` (already in client.go:272) which does ECDH + HKDF + AES-GCM per receiver.
   - Store the *encrypted* blob + nonce + sender pubkey in the inbox entry.
   - Receiver uses `DownloadAndDecrypt(senderPub)` (client.go:296).
   - The placeholder self-encryption at handlers.go:308 must be deleted and replaced with proper peer lookup + per-peer encryption (or envelope for multiple peers).

3. **Auto-push trigger**:
   - Extend the existing watcher change path (after redaction/processing, before or after history) to also call a new `pushToRelayPeers(format, data)` if `cfg.Relay.Enabled && len(peers) > 0`.
   - The push must be non-blocking (goroutine) and best-effort (log warnings on failure, never block clipboard read).

4. **CLI surface additions** (in `cmd/pastelocal/main.go`):
   - `pastelocal relay send <peer-fingerprint-or-id> [file]` (reads from clipboard or file, encrypts, uploads to inbox).
   - `pastelocal relay inbox` (list pending for me).
   - `pastelocal relay fetch <sender> [--out dir]` (decrypt + write file, print path).
   - `pastelocal relay status` (shows paired devices, last push, relay health).
   - Wire these into the existing `relayCmd` group.

5. **Remote helper extensions**:
   - `pastelocal-remote --relay <url> --send <path>` (symmetric to current `--send`).
   - Improve `--relay --list` and add `--relay --fetch <sender>` (currently only has fetch via download for self?).
   - Keep backward compat for SSH-tunnel mode.

6. **Grok skills packaging**:
   - Create `skill/grok/paste.md`, `paste-history.md`, `paste-send.md`, `paste-relay.md` (adapted from existing Claude skills but using Grok frontmatter: `allowed-tools`, `description`, clear step-by-step that works with Grok's Bash+Read contract).
   - Add a helper command or doc: `pastelocal install-grok-skills` that copies/symlinks them into `~/.grok/skills/pastelocal-paste/` etc. (or just prints the paths and instructions).
   - The skills should prefer SSH-tunnel when available, fall back to `--relay` if env/config says so.

7. **Watcher + Relay integration point**:
   - In `server.go` after a meaningful change is detected and processed, if relay client exists and auto-upload + peers configured → encrypt per peer and `Upload` to each peer's inbox (or use a single envelope upload if we extend the server).

8. **Relay server hardening for v1**:
   - Rate limiting per device/token (reuse or copy the existing `RateLimiter` pattern).
   - Max blob size, max inbox depth per device.
   - Auth on all mutating endpoints (already mostly there).
   - Structured JSON logging.
   - Health endpoint already returns counts — enhance it.

9. **Config**:
   - Add `relay.peers` or keep using the server-side peer table (client calls `AddPeer`).
   - Expose `relay.auto_upload_on_change = true` (default false for safety).

---

## 4. Detailed Phased Implementation Plan (follow in order)

**Phase 0 — Prep (small)**
- Add any tiny missing imports / test helpers.
- Create `internal/relay/persistence.go` (or `store.go`) with a clean `RelayStore` interface + file implementation.
- Update `cmd/relay-server/main.go` to accept `--state-dir` and use the store (refactor the giant Server struct to delegate blob/inbox/device/peer ops to the store).

**Phase 1 — Fix the crypto & daemon send path (highest priority)**
- Delete the placeholder at handlers.go:307-319.
- Add a method `(*Server) pushClipboardToRelayPeers(format string, data []byte)` that:
  1. Loads the list of peers for this device (via new relay client method or cached from config).
  2. For each peer, fetches the peer's public key (add `GetPeerPubKey` to client or store it at pairing time).
  3. Calls `relayClient.EncryptAndUpload(peerPub, format, data, ttl)`.
- Call this from the watcher change path (after `lastClipboardChange` update) and optionally from explicit POST /clipboard write.
- Make the upload goroutine + bounded concurrency.

**Phase 2 — Persistence layer + relay-server refactor**
- Define `type RelayStore interface { ... }` with methods for devices, peers, blobs, inbox.
- Implement `fileStore` that uses a directory:
  ```
  state/
    devices.json
    peers.json
    blobs/<id>.json
    inbox/<receiver>/<sender>.json  (or pointer)
  ```
  Or simpler single `state.json` with top-level maps (easier atomic rewrite).
- On every write path in handlers, go through the store.
- Implement `Load()`, `Save()`, `Compact()` (drop expired + rewrite).
- Startup: load, drop expired, start cleanup ticker.
- Keep the in-memory maps as hot cache; store is source of truth.
- Add integration test that restarts the relay-server process and verifies inbox items survive.

**Phase 3 — Complete CLI verbs + remote send path**
- Implement `runRelaySend`, `runRelayInbox`, `runRelayFetch` in main.go (parallel to existing relay* funcs).
- Wire `pastelocal relay send <peer> [--file path | --clipboard]` (default from local OS clipboard via the same reader used by daemon? Or require a file for now).
- Extend `pastelocal-remote` flags and add `runRelaySend(...)`.
- Update the skill templates to mention `--relay` variants.

**Phase 4 — Watcher auto-push + TUI/doctor/status**
- Wire watcher detection → relay push (guarded by config).
- Add relay status to `pastelocal` TUI dashboard (internal/tui/dashboard.go).
- Add doctor checks: "relay keypair present?", "can reach relay URL?", "has at least one peer?", "last successful push < 1h?".
- Expose `/relay/status` (or augment existing `/version` or `/health` on daemon) for the TUI.

**Phase 5 — Grok skills + packaging**
- Create `skill/grok/` directory mirroring the structure but with proper Grok SKILL.md frontmatter.
  Example skeleton for `skill/grok/paste/SKILL.md`:
  ```markdown
  ---
  name: paste
  description: Paste the image or text from the user's local clipboard (via PasteLocal relay or SSH tunnel) into this Grok session.
  allowed-tools: Bash(pastelocal-remote:*), Read(*), Bash(rm:*)
  metadata:
    short-description: "Bring laptop clipboard into remote Grok session"
  ---
  ... steps adapted from existing paste.md, with notes about preferring SSH vs --relay ...
  ```
- Add `pastelocal install-grok-skills` (or `pastelocal relay grok-setup`) that:
  - Creates `~/.grok/skills/pastelocal-paste/`, copies the SKILL.md + any helper script.
  - Prints "Add to your Grok config or just use /paste ...".
- Provide one helper Go/Python script if needed (e.g. a tiny wrapper that chooses SSH vs relay based on env).

**Phase 6 — Polish, docs, tests, acceptance**
- Update all READMEs, ARCHITECTURE, new `docs/RELAY.md` (extracted from this spec + examples).
- Add e2e test (can be in `e2e/` or new `test/relay_e2e_test.go`) that spins up relay-server in background, two fake "devices", exercises full round-trip.
- Update error codes if new ones needed.
- Run `make lint test` and fix.
- Security pass: constant-time everywhere, no token leakage in logs (already redacted in places), TTL enforcement, size limits on relay blobs.
- Final acceptance checklist (see section 8).

---

## 5. Specific File Change Map (implementer must touch or create these)

**New files (create):**
- `docs/RELAY.md` (user-facing guide, extracted + examples)
- `internal/relay/store.go` (interface + file implementation + tests)
- `skill/grok/paste/SKILL.md` (and siblings for history/send/relay)
- `cmd/pastelocal/grok.go` (or add to main.go) — the install command
- `internal/server/relay_push.go` (new helper for the push logic, keeps server.go clean)
- `test/relay_integration_test.go` (or extend existing)

**Heavy modification:**
- `cmd/relay-server/main.go` — refactor to use store, clean up duplicated handlers (the code after line 508 looks like appended patches — consolidate), add rate limiter, better logging, persistence flag.
- `internal/server/handlers.go:290-320` — replace placeholder, add push helper call.
- `internal/server/server.go` — wire new push method, expose relay peer status, start relay client with peers loaded.
- `cmd/pastelocal/main.go:1270+` — add 3-4 new subcommands + run* funcs (~150-250 lines).
- `cmd/pastelocal-remote/main.go` — add runRelaySend + improve relay flags/UX.
- `internal/config/config.go` — add a couple fields if needed (e.g. `Relay.AutoUploadOnChange`).
- `internal/tui/dashboard.go` — show relay section when enabled.
- `internal/doctor/checks.go` + `fixes.go` — new relay checks.
- `README.md` + `docs/ARCHITECTURE.md` — update status from "experimental" to "v1.0".

**Light touches:**
- `internal/proto/types.go` — maybe a RelayStatus or InboxItem if useful for daemon exposure.
- Error codes (`internal/errors/codes.go`).
- All four existing skills + new Grok ones for consistency.

---

## 6. Key Technical Snippets / Patterns to Reuse

- Rate limiter: copy pattern from `internal/server/ratelimit.go`
- Atomic write: look at `internal/sshconfig/atomic.go` for the proven `writeFileAtomic` helper.
- Crypto: `crypto.Encrypt`, `GenerateNonce`, `KeyPair.SharedSecret` + the envelope helpers.
- Watcher change notification: the exact spot after line 436 in watcher.go and the one in handlers.go:158.
- Existing `runSend` (remote) as template for `runRelaySend`.
- Doctor pattern in `internal/doctor/`.

**Important invariant:** Never block the clipboard read path on relay I/O. Always goroutine + timeout + best-effort.

---

## 7. Security & Threat Model Additions for Relay

- Relay server never sees plaintext (enforced by client-side encryption before upload).
- Inbox is per-receiver; a device can only fetch its own inbox items (auth + server-side check).
- Device key is 0600, like the SSH token.
- Add rate-limit per device on upload/inbox endpoints (prevent one compromised device from DoS'ing the relay).
- TTL + compaction prevents unbounded growth.
- Fingerprint is short (16 hex) for manual verification; full device ID for machine use.
- When adding peer, both sides should mutually add (or server can auto-mirror — decide in impl).

Run a security-auditor reviewer pass on the final crypto + auth paths.

---

## 8. Acceptance Checklist (the implementer + reviewers must verify every item)

- [ ] `pastelocal relay init` creates keypair + files with correct perms.
- [ ] `pair` registers, saves token, prints fingerprint.
- [ ] `add-peer` works both directions; `devices` and `inbox` reflect state.
- [ ] End-to-end: laptop clipboard change (with watcher+auto_upload) → appears in remote `pastelocal-remote --relay --list` within 10s.
- [ ] `pastelocal-remote --relay <url> --send foo.png` succeeds and shows up on the peer's inbox.
- [ ] Restart relay-server process → pending inbox items are still there and decryptable.
- [ ] Expired blobs are cleaned (TTL respected).
- [ ] Error paths: bad peer, expired token, oversized blob, rate limited — produce clear fix-hint errors.
- [ ] No plaintext ever leaves the client (audit the upload paths).
- [ ] Grok skills exist under `skill/grok/`, have correct frontmatter, and the install command (or doc) tells the user exactly how to activate them in Grok.
- [ ] TUI dashboard shows relay status when enabled.
- [ ] `pastelocal doctor` has at least 2 new relay-related checks (with --fix where sensible).
- [ ] All new code has unit tests (store, push logic, CLI parsing) + at least one integration that exercises persistence.
- [ ] `go test ./...`, `golangci-lint`, build, and manual round-trip on macOS all green.
- [ ] Docs updated so a new user following only README + RELAY.md can get multi-device working in < 5 minutes.
- [ ] No scope creep: only the items in this spec (no random "while I'm here" features).

---

## 9. Open Decisions for Implementer (document your choice in the summary)

1. Single `state.json` vs directory-of-files for the store? (Recommend single atomic JSON for v1 simplicity.)
2. Should the relay-server also support the legacy `/api/v1/upload` (self) path fully, or can we deprecate it?
3. How does a device discover the *public key* of a peer at send time? (Options: server returns it on `ListDevices`/`inbox list`, or we store it at `add-peer` time on the client side and pass it in upload. The client already has `ListDevicesResponse` with pubkeys — best to use/extend that.)
4. Default for `auto_upload_on_change` — false (safe) or true after first peer is added?
5. Do we expose a WebSocket or SSE on the relay-server for "new inbox item" push notifications, or keep pure polling for v1? (Recommend polling + good `--watch` UX for now.)

Document your choices clearly in the implementation summary written to the summary file.

---

## 10. How to Verify Locally While Building

```bash
# Terminal 1 — relay
go run ./cmd/relay-server --state-dir /tmp/relay-state --port 7332

# Terminal 2 — laptop simulation
go run ./cmd/pastelocal relay init
go run ./cmd/pastelocal relay pair http://localhost:7332
# ... make a clipboard change or use --send

# Terminal 3 — remote simulation (different device key)
# (copy device-key to another dir or use env override if you add one)
go run ./cmd/pastelocal-remote --relay http://localhost:7332 --list
```

Add a `make relay-demo` target if it helps.

---

**This spec is complete and self-contained.** Every necessary context, gap analysis, and acceptance gate is here. The implementer should treat deviations only after explicit justification in the summary.

Good luck — make the relay *just work* so the future vision becomes the present. The user will wake up to a working multi-device clipboard bus + Grok skills.

— Grok, signing off for the night.
