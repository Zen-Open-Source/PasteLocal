---
description: Send a file from this remote host to the user's local clipboard.
allowed-tools: Bash(pastelocal-remote:*)
---

Send a file from this remote host to the user's local clipboard.

1. Determine the file path to send. The user should provide it, or you should ask.
2. Detect the format: use "png" for image files (png, jpg, gif, bmp, heic) and "text" for text files.
3. Run `pastelocal-remote --send <path> [--send-format <format>]` via the Bash tool.
4. If the command fails (non-zero exit), report the exact stderr to the user verbatim and stop. Do not retry.
5. Confirm to the user with one line: "Sent to your local clipboard."
