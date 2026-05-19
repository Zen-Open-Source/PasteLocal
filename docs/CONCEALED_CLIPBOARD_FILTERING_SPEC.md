# PasteLocal — Concealed / Sensitive Clipboard Filtering
**Implementation Specification for /implement**

**Prepared for:** Morgan Linton  
**Based on:** Feature request from @dedene on X  
**Date:** 2026-05-18  
**Goal:** Allow users to safely use the clipboard watcher + relay without accidentally sending password manager secrets (1Password, Bitwarden, etc.) to remote machines.

---

## 1. Executive Summary & Motivation

The clipboard watcher (`watch.enabled = true`) is one of PasteLocal’s most powerful features — it automatically detects changes on the local machine and makes the latest clipboard content available on remotes via the relay or SSH tunnel.

However, this creates a real trust problem for power users:

> “I’m reluctant to let a local daemon watch my clipboard… knowing every secret key I copied [from 1Password] is also sent to my remote.”

Password managers (especially 1Password on macOS) deliberately mark sensitive items using the standard **Concealed pasteboard type** (`org.nspasteboard.ConcealedType`) so that good clipboard managers (Raycast, Paste, Maccy, etc.) can automatically exclude them from history.

**Current state:** PasteLocal’s watcher does **not** respect this signal. It will happily relay a password that was just autofilled by 1Password.

**Proposed feature:** Add first-class support for detecting and filtering “concealed” / sensitive clipboard items, modeled after the behavior users already expect from Raycast and similar tools.

This is a **trust and safety** feature, not just a nice-to-have. Without it, many careful users will keep the watcher disabled.

---

## 2. Current State Assessment

### Existing Redaction System
- `internal/server/redaction.go` + `RedactionConfig` in `config.go`
- Supports regex rules with actions `"redact"` or `"block"`
- Default rules for AWS keys, GitHub tokens, etc.
- Applied in the read path (`handlers.go`) for both text and (heuristic) images
- **Limitation:** Purely content-based. It cannot reliably catch arbitrary passwords or passphrases that password managers put on the clipboard.

### Watcher
- `watch.enabled` + `startClipboardWatcher()` in `server.go`
- Polls the OS clipboard and triggers on meaningful changes
- Currently has no concept of “this item is marked as secret by the source app”

### macOS Clipboard Reader
- Currently uses external tools (`pngpaste` + `pbpaste`)
- These tools lose pasteboard item type metadata (`NSPasteboardItem` types)
- Native detection of `ConcealedType` is **not possible** with the current approach

---

## 3. Goals & Non-Goals

### Goals (v1 of this feature)
- Detect when a clipboard item was written as “concealed” (primarily macOS + 1Password, but extensible)
- By default, **prevent** concealed items from being:
  - Auto-uploaded via the watcher to the relay
  - Returned by `GET /clipboard` when the watcher triggered the read
- Make behavior configurable (block / warn / allow / redact-if-possible)
- Provide clear, non-alarmist logging and TUI feedback when something is filtered
- Work alongside (not replace) the existing regex redaction system

### Non-Goals (for v1)
- OCR + semantic analysis of screenshots to find passwords
- Cross-platform perfect detection on Linux/Windows (we can do best-effort)
- Automatically deleting the item from the local clipboard
- Per-password “one-time allow” flows in v1
- Deep integration with specific password managers beyond the standard ConcealedType

---

## 4. Proposed Design

### 4.1 Core Concept: “Concealed” Items

On macOS, password managers write sensitive content using the pasteboard type:

- `org.nspasteboard.ConcealedType`

When this type is present on the `NSPasteboardItem`, the item is considered a secret by convention. Good tools (Raycast, etc.) skip these items for history.

We will treat the presence of this type (or equivalent signals on other platforms) as a strong signal that the user does **not** want this content relayed.

### 4.2 Layered Approach (Defense in Depth)

1. **Source Signal (best)** — Native detection of ConcealedType on macOS
2. **Content Heuristics (second layer)** — Improve/expand existing redaction rules (long random strings, passphrases, common secret formats)
3. **App Awareness (future / optional)** — Optionally track which app last wrote to the clipboard (via `NSPasteboard` or Accessibility) and allow users to mark certain apps as “always sensitive”
4. **User Rules** — Keep the existing powerful regex system and make it easy to add custom “never relay” patterns

### 4.3 User-Facing Configuration

Proposed new config section (under `[watch]` or top-level):

```toml
[watch]
enabled = true

[watch.sensitive]
filter_concealed = true                    # macOS ConcealedType + equivalents
block_on_match = true                      # block the read entirely (default)
# redaction_mode = "block"                 # or "redact" (risky for secrets)
log_filtered_items = true                  # show in TUI / logs when something was filtered
```

Also allow per-host overrides later (some remotes are more trusted).

### 4.4 UX & Transparency

When a sensitive item is filtered:

- Log clearly: `"Filtered concealed clipboard item (source: 1Password / ConcealedType)"`
- TUI / `pastelocal status` should surface recent filtered events
- Do **not** spam the user on every password fill — be quiet by default but auditable

---

## 5. Implementation Plan (Phased)

### Phase 0 — Research & Design (small)
- Confirm exact macOS pasteboard type constants and how `NSPasteboardItem` exposes them
- Evaluate best native Go library (or small cgo helper) for rich pasteboard reading on macOS
  - Candidates: direct cgo, `github.com/gorilla/websocket` no — better to look at `golang.design/x/clipboard`, `atotto/clipboard` fork, or a tiny Objective-C bridge

### Phase 1 — Native macOS Concealed Detection (core)
- Add a new `richClipboardReader` (or extend `macOSReader`) that can read full `NSPasteboardItem` types
- Detect `org.nspasteboard.ConcealedType` (and the older `NSPasteboardTypeConcealed` if relevant)
- When detected:
  - In watcher path: skip the change entirely (do not update `lastClipboardChange`, do not push)
  - In explicit `/clipboard` read path: return a new error code `CB1011` (“Content filtered as sensitive / concealed”)
- Add config toggle `filter_concealed`

### Phase 2 — Improve Heuristic + Rule Engine
- Expand default redaction rules with common “looks like a random secret” patterns (long base64, high-entropy strings, etc.)
- Consider a “high entropy” heuristic for text (optional, behind flag)
- Make it easy for users to add their own “never relay if matches” rules

### Phase 3 — Transparency & Polish
- Add filtered event to history / TUI (with reason: “concealed”, “redaction rule”, etc.)
- Add `pastelocal relay status` or enhanced doctor output showing recent sensitive filters
- Document the feature prominently in `RELAY.md` and security section
- Consider a one-time “I understand the risks” note on first use of watcher + relay

### Phase 4 — Testing & Edge Cases
- Unit tests for the new pasteboard type detection (mock or recorded pasteboard states)
- Integration test: simulate writing a concealed item and confirm it is not relayed
- Manual testing with real 1Password autofill flows

---

## 6. Key Technical Decisions (to be confirmed or chosen during implementation)

1. **Native vs external tools for macOS reading**
   - Recommendation: Move (or add a parallel path) to native `NSPasteboard` access for rich metadata. External `pbpaste` is insufficient for ConcealedType detection.

2. **Blocking vs Redacting secrets**
   - Strong recommendation: **Block** by default for anything marked concealed. Redacting a password is usually pointless and potentially dangerous.

3. **Where the decision is made**
   - Best: Early in the watcher (before we even spend time reading full image content).
   - Also enforce at the HTTP handler level for defense in depth.

4. **Cross-platform story**
   - macOS: first-class via ConcealedType
   - Linux: best-effort via existing redaction + possible future `wl-clipboard` metadata or `secret` mime types
   - Windows: similar best-effort

5. **Audit / history of filtered items**
   - Should filtered items appear in `pastelocal-remote --list` at all? (Recommendation: no, or only as “1 filtered sensitive item” without content)

---

## 7. Files Likely to Change

**New / Major:**
- `internal/clipboard/macos_rich.go` (or refactor of `macos.go`) — native pasteboard reader with type inspection
- `internal/server/sensitive.go` — new helper for “is this item sensitive/concealed?”
- Possibly a small Cgo / Objective-C helper if we go fully native

**Modified:**
- `internal/config/config.go` — new `Watch.Sensitive` section
- `internal/server/server.go` + `watcher.go` — early filtering in the watcher loop
- `internal/server/handlers.go` — return new error code when a read is blocked due to sensitivity
- `internal/server/redaction.go` — possible enhancements to heuristics
- `internal/errors/codes.go` — new error code (e.g. `CB1011`)
- `internal/tui/dashboard.go` + doctor — show filtered sensitive events
- `docs/RELAY.md` and `SECURITY.md` — documentation
- `README.md` — mention the new safety feature

---

## 8. Security & Threat Model Considerations

- We are **reducing** the blast radius of the watcher, which is the correct direction.
- False negatives (missing a secret) are worse than false positives (blocking something the user wanted). Err on the side of caution.
- The feature must be hard to accidentally disable for the main use case.
- We should be transparent so users understand what is being filtered.

---

## 9. Acceptance Criteria (for the implementer + reviewers)

- [ ] On macOS, when 1Password (or any app) writes a password using the Concealed pasteboard type, the watcher does **not** make that item available to remotes.
- [ ] Explicit `pastelocal-remote` or `/clipboard` calls for a concealed item return a clear, actionable error.
- [ ] Existing regex redaction continues to work as a second layer.
- [ ] Behavior is configurable and the defaults are safe.
- [ ] Users can see (in logs / TUI) when sensitive items were filtered, without leaking the secrets.
- [ ] Documentation clearly explains the feature and how to opt out if desired (for advanced users).
- [ ] No regression in normal (non-sensitive) clipboard watching / relay behavior.

---

## 10. Open Questions for the Spec / Implementation

1. Exact constant name and how to read it reliably from Go on modern macOS (`org.nspasteboard.ConcealedType` vs others)?
2. Should we also respect the older `NSPasteboardTypeConcealed` for maximum compatibility?
3. Do we want a “temporarily allow next sensitive item” command / signal?
4. How loud should the filtering be in the TUI by default?
5. Should we add a one-time onboarding note the first time watcher + relay + sensitive filtering is active?

---

This spec is ready to be turned into a detailed implementation plan. Once you’re happy with the direction, we can refine any of the open questions and then you can feed the final version into `/implement` (with `--effort 3` or `4` recommended, given the security + platform-native aspects).

Would you like me to expand any section (especially the technical macOS detection approach or the exact config surface) before you use it? Or shall I turn this into the final ready-to-paste spec?