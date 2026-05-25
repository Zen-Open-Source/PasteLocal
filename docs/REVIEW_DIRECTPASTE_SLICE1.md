# DirectPaste v1 — First Review Slice (Protocol + Basic Flow)

**Status:** Ready for first combined /implement + /check review cycle

## What This Slice Delivers

This is the initial end-to-end slice of the **Native Direct Paste (DirectPaste v1)** feature.

### Changes

**Protocol (new types in `internal/proto/types.go`)**
- `DirectPasteRequest`
- `DirectPasteAccepted`
- `DirectPasteProgress`
- `DirectPasteComplete`
- `DirectPasteError`

**Daemon side**
- New route: `POST /clipboard/direct-paste`
- Working handler (`handleDirectPaste`) that accepts a request and returns the full message sequence:
  - `paste_accepted`
  - `paste_progress` (simulated)
  - `paste_complete` (with realistic remote path + analysis sidecar)

**Client side (`pastelocal-remote`)**
- New flag: `--test-direct-paste`
- Helper that posts a request and pretty-prints the protocol responses

**Documentation**
- `docs/DIRECTPASTE_PROTOCOL.md` — first draft of the protocol spec (messages, flow, error codes, security principles, MVP scope)

## Goals of This Review (/check focus)

Please evaluate the following with a production-grade lens:

1. **Protocol Design**
   - Are the message shapes clear and minimal?
   - Is the request/response model (especially the multi-JSON streaming style for demo) reasonable?
   - Any obvious missing fields or security concerns?

2. **Security Surface**
   - Does the new handler properly reuse existing auth (`validateAuth`)?
   - Any new attack vectors introduced by the `/clipboard/direct-paste` endpoint?
   - Rate limiting / size limits still enforced?

3. **Extensibility**
   - Is the protocol easy for third parties (Termius, iTerm2, Kitty, WezTerm, etc.) to implement?
   - Does the draft spec in `DIRECTPASTE_PROTOCOL.md` feel sufficient?

4. **Implementation Quality**
   - Code style, error handling, logging consistent with the rest of the project?
   - Any obvious bugs or foot-guns in the current simulated handler?

5. **Direction Check**
   - Does this feel like the right foundation before we invest in real clipboard reading + actual byte streaming + tmux/zellij plugins?

## How to Review

- Run the client test:
  ```bash
  ./bin/pastelocal-remote --test-direct-paste
  ```
- Inspect the protocol document.
- Look at the diff for the four modified files + the new doc.

This slice is intentionally small so we can get early feedback on the protocol shape before building the full listener + plugins in Pass 2.

## Next Steps After This Review

- Incorporate feedback into the protocol (if any).
- Proceed to real implementation (actual clipboard read + streaming) in the remainder of Pass 1 / early Pass 2.
- Then move to the tmux + zellij reference implementations.

---

**Reviewers:** Please be rigorous. We are using the combined `/implement + /check` discipline for this feature to reach true production grade before any push.

Thank you!