---
name: paste-send
description: Send a file from this remote machine back to the user's local laptop clipboard via PasteLocal (SSH or relay).
allowed-tools: Bash(pastelocal-remote:*)
metadata:
  short-description: "Push file from remote to laptop clipboard"
---

# PasteLocal /paste-send — Grok Skill

Push a file (image or text) from the remote machine to the user's local laptop clipboard.

## Steps
1. Ask the user (or infer from context) which file to send.
2. Detect format: png/jpg/etc → "png", everything else → "text".
3. Run:
   - SSH path: `pastelocal-remote --send /path/to/file --send-format png`
   - Relay path: `pastelocal-remote --relay <url> --send /path/to/file`
4. On success the command prints confirmation. Tell the user: "Sent to your local clipboard."
5. On error, report exact stderr.

This pairs perfectly with `/paste` for bidirectional flow.
