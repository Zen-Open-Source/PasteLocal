---
name: paste
description: >
  Paste the latest image or text from the user's local laptop clipboard into this
  remote Grok session using PasteLocal (SSH tunnel or relay).
allowed-tools: Bash(pastelocal-remote:*), Read(*), Bash(rm:*)
metadata:
  short-description: "Bring laptop screenshots/text into remote Grok via PasteLocal"
  category: "clipboard"
---

# PasteLocal /paste — Grok Skill

Bring your laptop's clipboard (especially screenshots) into a remote machine where Grok is running.

## When to use
- User says "paste the screenshot", "grab what's on my clipboard", "show me the image from my laptop", etc.
- Works over existing SSH (preferred) or the experimental relay.

## Steps (execute in order)

1. **Try the SSH-tunnel path first** (fastest, most common):
   - Run `pastelocal-remote` via the Bash tool.
   - It will print a file path on success (e.g. `/home/user/.cache/pastelocal/pastelocal-abc123.png`).

2. **If the SSH path fails or user explicitly wants relay** (no direct tunnel or multi-device scenario):
   - Run `pastelocal-remote --relay <url>` (user must provide the relay URL or it is in their config).
   - Or `pastelocal-remote --relay http://localhost:7332 --list` to see options, then fetch a specific one.
   - The command still prints a file path.

3. **On any failure (non-zero exit)**: Report the **exact** stderr to the user verbatim and stop. Do not retry or guess.

4. **Read the file** using your Read tool on the exact path returned.

5. **VisionPaste enrichment (v2 proactive & cached)**: If the clipboard item was an image/screenshot and the local daemon has `[vision]` analysis configured (e.g. tesseract OCR), a companion sidecar file with the same base name but `.analysis.txt` suffix will exist next to the image (e.g. `...-abc123.analysis.txt`). Read it too — it contains pre-extracted OCR text and/or a natural-language description (now produced proactively by watcher for instant availability). Present this rich text context to the model *first* (before or alongside the image); it dramatically improves token efficiency and accuracy for code, errors, UI, diagrams etc. The image remains available via your vision capabilities for anything the text missed.

6. **Clean up**: Immediately run `rm <path>` and (if present) `rm <analysis-sidecar>` (use Bash) so temp files do not linger.

7. **Confirm** with one short line:
   - Image: "Got it, image attached from your laptop clipboard."
   - Text: "Got it, text attached from your laptop clipboard."
   - If it came via relay: "Got it via relay, image attached."

## Tips for great UX
- If the user has multiple devices paired via relay, you may need to ask "which one?" and use `--list`.
- With VisionPaste enabled on the user's daemon, screenshots arrive with rich OCR + description context already attached — use it immediately instead of spending extra turns on vision analysis.
- For repeated use, suggest the user saves a named snippet locally with `pastelocal snippets save my-design`.

This skill is the Grok-native equivalent of the Claude `~/.claude/commands/paste.md`. It prefers the zero-config SSH experience when available.
