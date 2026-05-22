---
name: paste-snippet
description: >
  Paste a named snippet previously saved from the user's local laptop clipboard into this
  remote Grok session using PasteLocal (SSH or relay).
allowed-tools: Bash(pastelocal-remote:*), Read(*), Bash(rm:*)
metadata:
  short-description: "Recall a saved named snippet (e.g. api-key, template) from laptop"
  category: "clipboard"
---

# PasteLocal /paste-snippet — Grok Skill

Fetch and paste a user-saved named snippet (text or image) that was previously captured with `pastelocal snippets save <name>` on the laptop.

## When to use
- User refers to "my deploy command snippet", "the api-key template", "that standard header I saved as 'license'".
- Replaces typing or copy-paste from notes; snippets survive clipboard churn.
- `pastelocal-remote --snippet NAME` works over SSH (preferred); for relay setups use plain `--relay` for recent auto-uploaded items (direct --snippet + --relay not supported in dispatch).

## Steps (execute in order)

1. **If user did not provide a name**: list available snippets first.
   - SSH: `pastelocal-remote --list-snippets`
   - Suggest common ones the user may have: deploy, header, license, etc.

2. **Run the fetch**:
   - SSH path: `pastelocal-remote --snippet <name>`
   - Relay path (multi-device without SSH): recent auto-uploaded items via plain `pastelocal-remote --relay <url>` (full --snippet + --relay combo not supported in current remote dispatch; copy snippet locally then send via relay or use SSH for direct daemon access).

3. **On any failure (non-zero exit)**: Report the **exact** stderr verbatim and stop.

4. **Read the file** using Read tool on the returned path.

5. **Clean up** with `rm <path>` (and sidecar if present).

6. **Confirm**:
   - "Pasted snippet '<name>' (text attached)."
   - "Pasted snippet '<name>' (image attached)."

## Tips for great UX
- Snippets are stored locally on the laptop daemon (not in relay inbox unless explicitly sent).
- For relay-heavy workflows, the user can `pastelocal relay send` a snippet or enable auto-upload and copy the snippet locally first.
- Combine with `/paste` for ad-hoc and `/paste-history` for time-based recall.
- User manages snippets with `pastelocal snippets save|list|delete <name>` on laptop.

This completes the trio of clipboard ingestion skills (/paste + /paste-history + /paste-snippet) for Grok.
