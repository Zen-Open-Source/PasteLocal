---
description: Paste the image or text from the user's local clipboard into this session.
allowed-tools: Bash(pastelocal-remote:*), Read(*), Bash(rm:*)
---

Paste the image or text from the user's local clipboard.

1. Run `pastelocal-remote` via the Bash tool. It prints a file path on success.
2. If the command fails (non-zero exit), report the exact stderr to the user verbatim and stop. Do not retry.
3. Use the Read tool on the printed path.
4. **VisionPaste (v1)**: For images, also check for and Read the companion `.analysis.txt` sidecar (same basename) if present — it contains automatic OCR text and description of the screenshot. Feed the text content to the model first for best results.
5. After Read succeeds, run `rm <path>` (and the `.analysis.txt` if it was read) to clean up.
6. Confirm to the user with one line:
   - For images: "Got it, image attached."
   - For text: "Got it, text attached."
