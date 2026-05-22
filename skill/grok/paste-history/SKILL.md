---
name: paste-history
description: >
  Paste a previous clipboard entry from the user's local laptop history into this
  remote Grok session using PasteLocal (SSH tunnel preferred; relay for recent pushed items).
allowed-tools: Bash(pastelocal-remote:*), Read(*), Bash(rm:*)
metadata:
  short-description: "Bring past laptop screenshots/text from clipboard history into remote Grok"
  category: "clipboard"
---

# PasteLocal /paste-history — Grok Skill

Bring a previous entry from the laptop's clipboard history (text or screenshot) into the remote session. Supports `--list` + `--index`, `--search` + `--id` (Recall-powered), and relay fallback for recent items.

## When to use
- User says "paste the previous screenshot", "grab that error from history", "the docker log I copied 5 minutes ago", "search my clipboard for the nginx config".
- The plain `/paste` (latest only) is insufficient.
- Works best over SSH tunnel; relay path provides recent auto-uploaded items.

## Steps (execute in order)

1. **Try the SSH-tunnel path first** (full history + search):
   - Use `pastelocal-remote --list` to see recent, then `pastelocal-remote --index N`
   - Or better for agents: `pastelocal-remote --search "the docker volume error" --limit 5` then `pastelocal-remote --id <stable-id>`
   - The command prints a file path (and optional `.analysis.txt` sidecar for images with VisionPaste).

2. **If the SSH path fails or in pure-relay multi-device setup** (no direct tunnel):
   - Use `pastelocal-remote --relay <url>` (fetches the most recent pending item pushed via auto-upload or manual send).
   - Relay provides only the most recent pushed item via plain `--relay` (advanced --list/--search/--id over relay not directly supported; use SSH for full history or ask user to push the desired item via relay send).

3. **On any failure (non-zero exit)**: Report the **exact** stderr to the user verbatim and stop. Do not retry or guess.

4. **Read the file** using your Read tool on the exact path returned.

5. **VisionPaste enrichment**: If image and analysis sidecar exists, read the `.analysis.txt` first for OCR/description.

6. **Clean up**: Immediately run `rm <path>` and sidecar.

7. **Confirm** with one short line:
   - "Got it, image attached from history."
   - "Got it, text attached from history."
   - If via relay: "Got it via relay from recent history."

## Tips for great UX
- Prefer `--id` (from search) over `--index` because list order can shift.
- `--search` requires `[recall]` enabled on the laptop daemon + embeddings.
- Concealed items (password popups) are filtered and never appear in history or relay uploads.
- For named reusable items, suggest user saves with `pastelocal snippets save <name>` and use `/paste-snippet` instead.
- Pairs with `/recall` for pure semantic search UX.

This is the Grok-native history equivalent. It prefers SSH for complete access and falls back gracefully for relay-based recent items.
