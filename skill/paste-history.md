---
description: Paste a previous clipboard entry from history by selecting from recent items.
allowed-tools: Bash(pastelocal-remote:*), Read(*), Bash(rm:*)
---

Paste a previous clipboard entry from history.

1. Run `pastelocal-remote --list` via the Bash tool to see available history entries.
2. Ask the user which entry they want (by index number, where 1 is most recent).
3. Run `pastelocal-remote --list --index <number>` via Bash to fetch that entry.
4. If the command fails (non-zero exit), report the exact stderr to the user verbatim and stop. Do not retry.
5. Use the Read tool on the printed path.
6. After Read succeeds, run `rm <path>` to clean up.
7. Confirm to the user with one line:
   - For images: "Got it, image attached from history."
   - For text: "Got it, text attached from history."
