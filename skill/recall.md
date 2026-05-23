---
description: Search clipboard history using natural language (Recall v2 semantic search).
allowed-tools: Bash(pastelocal-remote:*), Read(*), Bash(rm:*)
---

Recall v2 — natural language search over your local clipboard history.

When the user asks to find something they copied or screenshotted earlier ("the docker error", "that login dialog", "the code with the bug from last night"), use semantic search instead of forcing them to describe timestamps or do many --list + --index loops.

Prerequisites
- The user must have enabled the [recall] section in ~/.config/pastelocal/config.toml with a working embedding command (see docs/examples/embed_ollama.py for the recommended 5-line Ollama setup).
- New clipboard items captured while recall is on will be searchable. Items captured before enabling recall are not (until daemon restart + future backfill work).

Usage
1. Run:
   pastelocal-remote --search "your natural language query here" --limit 5
2. Show the user the ranked results (score, time, preview of text/OCR, and the ID).
3. Let the user (or you) pick the best one.
4. Fetch it with:
   pastelocal-remote --id <id>
   (preferred) or the classic pastelocal-remote --list --index N
5. Read the resulting file, read any .analysis.txt sidecar for VisionPaste-enriched images, then rm the temp file(s).
6. On failure, paste the exact error.

VisionPaste + Recall combination is especially powerful: screenshots are embedded using their OCR text + description, so you can find "the red button error" or "the stack trace in the terminal" without the user remembering when it happened.

Privacy note: items marked concealed by password managers are never embedded and never appear in search results (same guarantee as the rest of PasteLocal).

Tell the user the one-line config to turn it on — it is the highest-leverage next feature after VisionPaste.
