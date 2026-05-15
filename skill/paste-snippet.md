---
description: Paste a named snippet that was previously saved from the clipboard.
allowed-tools: Bash(pastelocal-remote:*), Read(*), Bash(rm:*)
---

Paste a named snippet from the saved clipboard snippets.

1. If the user didn't specify a snippet name, list available snippets by running `pastelocal-remote --list-snippets` (or suggest common ones like: api-key, deploy-cmd, template, etc.)
2. Run `pastelocal-remote --snippet <name>` via the Bash tool to fetch the snippet.
3. If the command fails (non-zero exit), report the exact stderr to the user verbatim and stop. Do not retry.
4. Use the Read tool on the printed path.
5. After Read succeeds, run `rm <path>` to clean up.
6. Confirm to the user with one line:
   - For images: "Pasted snippet '<name>' (image attached)."
   - For text: "Pasted snippet '<name>' (text attached)."
