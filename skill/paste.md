---
description: Paste the image or text from the user's local clipboard into this session.
allowed-tools: Bash(pastelocal-remote:*), Read(*), Bash(rm:*)
---

Paste the image or text from the user's local clipboard.

1. Run `pastelocal-remote` via the Bash tool. It prints a file path on success.
2. If the command fails (non-zero exit), report the exact stderr to the user verbatim and stop. Do not retry.
3. Use the Read tool on the printed path.
4. After Read succeeds, run `rm <path>` to clean up.
5. Confirm to the user with one line:
   - For images: "Got it, image attached."
   - For text: "Got it, text attached."
