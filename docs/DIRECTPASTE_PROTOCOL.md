# DirectPaste Protocol v1 (Draft)

**Status:** Early design draft for DirectPaste v1  
**Goal:** Allow a remote terminal / SSH client to trigger a "native paste" action that pulls the user's local clipboard (text or image) over the existing authenticated PasteLocal connection, with progress reporting for large payloads (especially screenshots).

This document defines the wire messages and flow. It is intentionally simple and reuses the existing authentication and transport (SSH RemoteForward to `pastelocald` or the Relay).

---

## Core Principles

1. **No new trust boundaries** — Everything still flows over an already-authenticated PasteLocal connection (the same one used by `pastelocal-remote` today).
2. **Progress for images** — Large screenshots must show progress; the experience must feel native.
3. **Fail safe** — If anything goes wrong, fall back to the existing explicit `pastelocal-remote` flow.
4. **Minimal new surface** — Reuse as much of the existing watcher, auth, rate limiting, and concealed filtering as possible.
5. **Third-party friendly** — The protocol must be simple enough for Termius, iTerm2, Kitty, WezTerm, etc. to implement.

---

## High-Level Flow (Happy Path – Image)

1. User is connected to a remote machine via Termius / tmux / zellij.
2. User performs a "paste" action in the terminal (normal paste key, right-click, etc.).
3. The terminal client / plugin detects this is a DirectPaste-capable session and sends a **PasteRequest** to the remote listener.
4. The remote listener forwards the request (over the existing SSH tunnel or relay) to the local `pastelocald`.
5. `pastelocald`:
   - Validates the request (auth + rate limits)
   - Reads the local OS clipboard (respecting concealed/sensitive filtering)
   - If the content is an image, begins streaming it in chunks with progress updates
   - Writes the final file locally on the remote (same location and permissions as today)
   - Returns the absolute path + optional `.analysis.txt` sidecar info
6. Progress is shown in the terminal / status bar.
7. On completion, the path is printed (or injected) so the agentic tool can consume it immediately.
8. Cleanup is handled the same way as the current skill flow.

---

## Message Types (JSON over the existing authenticated channel)

All messages are sent as JSON objects. The transport can be:
- A new dedicated WebSocket (recommended long-term)
- Or piggy-backed on the existing `/clipboard/watch` WebSocket with a new message type (simpler for MVP)

### From Remote → Local (requests)

**PasteRequest**

```json
{
  "type": "paste_request",
  "request_id": "uuid-or-monotonic-id",
  "pane_id": "optional-tmux-or-zellij-pane-identifier",
  "client_info": {
    "name": "tmux-plugin" | "zellij-plugin" | "termius",
    "version": "x.y.z"
  }
}
```

### From Local → Remote (responses & progress)

**PasteAccepted**

```json
{
  "type": "paste_accepted",
  "request_id": "...",
  "format": "text" | "png",
  "size": 12345,
  "estimated_chunks": 12
}
```

**PasteProgress** (only for large payloads)

```json
{
  "type": "paste_progress",
  "request_id": "...",
  "bytes_sent": 8450000,
  "total_bytes": 12400000,
  "percent": 68
}
```

**PasteComplete**

```json
{
  "type": "paste_complete",
  "request_id": "...",
  "path": "/home/user/.cache/pastelocal/pastelocal-abc123.png",
  "analysis_path": "/home/user/.cache/pastelocal/pastelocal-abc123.analysis.txt",  // if Vision was available
  "format": "png",
  "byte_count": 12400000
}
```

**PasteError**

```json
{
  "type": "paste_error",
  "request_id": "...",
  "code": "DP1001" | "DP1002" | ...,
  "message": "human readable",
  "recoverable": true
}
```

---

## Error Codes (initial set)

- `DP1001` – Clipboard is empty or too large
- `DP1002` – Rate limit exceeded for direct paste
- `DP1003` – Concealed content (password manager) – not allowed via native paste
- `DP1004` – Client not authorized for direct paste (future fine-grained permissions)
- `DP1999` – Internal / transient error (fall back to explicit `pastelocal-remote`)

---

## Security & Privacy Notes

- All requests must carry the same Bearer token used by `pastelocal-remote` today.
- Concealed clipboard items are **never** delivered via DirectPaste (same rule as today).
- Rate limiting on the new path is mandatory (reuse or extend existing `RateLimiter`).
- The local daemon must still enforce `max_image_bytes`.

---

## MVP Scope (for the first /implement + /check run)

- Protocol as defined above (text + PNG with progress).
- Remote listener that can be triggered from tmux and zellij plugins.
- One reference tmux plugin that makes normal paste bring in the local clipboard.
- One reference zellij plugin.
- Updated Termius documentation + a working proof-of-concept.
- Doctor + TUI visibility.
- Clear `DIRECTPASTE_PROTOCOL.md` that third parties can implement against.

Non-goals for v1 (documented for later):
- Bidirectional "send from remote feels native"
- Full OSC52 replacement for text
- GUI SSH client deep integration (beyond Termius instructions)
- History / Recall integration at the paste moment

---

## Open Questions (to be resolved during Pass 1 design)

- Should we use a completely new WebSocket endpoint (`/clipboard/direct-paste`) or extend the existing watch connection?
- How does the remote listener authenticate to the local daemon (same token file is the simplest).
- Progress granularity for very large images (chunk size, throttling).

This document will be updated as the design is refined during the implementation passes.

---

*This is a living draft. The final version shipped with DirectPaste v1 will be authoritative for third-party implementers.*