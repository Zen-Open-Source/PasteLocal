---
name: recall
description: >
  Use natural language to search the user's local clipboard history (Recall v1).
  Finds the most relevant past screenshot or text even when you don't remember the exact time.
allowed-tools: Bash(pastelocal-remote:*), Read(*), Bash(rm:*)
metadata:
  short-description: "Semantic search over clipboard history using Recall v1"
  category: "clipboard"
---

# PasteLocal /recall — Grok Skill (Recall v1)

Search the entire clipboard history using natural language instead of manually running `--list` and guessing indices. Powered by user-configured local embeddings (e.g. Ollama) + VisionPaste OCR text for screenshots.

## When to use
- User says "find the docker error from yesterday", "where was that screenshot of the login screen", "recall the code snippet I copied earlier", "search my clipboard for the stack trace".
- The classic `--list` + `--index` flow is too many round trips for agents.
- Works only after the user has enabled `[recall]` in their local `config.toml` and captured some items while it was on.

## Prerequisites (tell the user if missing)
- `[recall]` section in `~/.config/pastelocal/config.toml` with `enabled = true` and a working `command`.
- Example quick start for the user (copy-paste):
  ```toml
  [recall]
  enabled = true
  timeout_seconds = 30
  command = "python3 -u ~/.config/pastelocal/embed_ollama.py"
  ```
- The example script (and a pure-Python alternative) live in the PasteLocal repo under `docs/examples/`.

## Steps (execute in order)
1. **Run the search**:
   ```
   pastelocal-remote --search "the docker compose error with the volume mount" --limit 5
   ```
   (limit is optional, default 5, max 20)

2. **Present the results to the user** (or the agent decides):
   - Show the ranked list with scores, timestamps, short preview text (OCR for images!), and the stable ID.
   - Ask the user (or autonomously pick) which one is correct.

3. **Fetch the chosen entry** (best UX):
   ```
   pastelocal-remote --id <the-id-from-search>
   ```
   This writes the file (and `.analysis.txt` sidecar if it was an image with VisionPaste).

4. **Read + clean up** (exactly like `/paste`):
   - Read the printed path with your Read tool.
   - If a `.analysis.txt` exists next to it, read that first and present the rich OCR/description to the model.
   - `rm` the file(s).

5. On any non-zero exit from pastelocal-remote, report the **exact** stderr and stop.

## Example dialogue the skill enables
User: "I had a weird nginx config error last night, can you grab the screenshot?"
Agent: runs `--search "nginx config error"`, shows top result with preview of the error text from OCR, user confirms, agent does `--id 174...-png`, reads it, and solves the problem.

## Tips for great UX
- Always prefer `--id` after search (stable, doesn't depend on list ordering).
- VisionPaste + Recall is magical for screenshots: the OCR + description text is what gets embedded, so "the red error dialog" works great.
- If no results: remind the user to enable recall + run a few reads so items get indexed.
- Concealed/sensitive items (password manager popups) are never embedded or returned — privacy by design.
- The feature is 100% local and private; the embedding command is fully under user control.

This is the Grok-native equivalent of the Claude `recall` command. It turns linear history into a true semantic memory for agents.
