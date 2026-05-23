# Grok-Native PasteLocal Skills

These are first-class Grok skills (SKILL.md + optional helpers) for using PasteLocal from inside Grok sessions on remote machines.

## Installation
Run from the PasteLocal source tree (or after cloning the repo):

```bash
pastelocal grok install-skills
```

This copies all skills into `~/.grok/skills/pastelocal-*` so Grok discovers `/paste`, `/recall`, etc.

## Available Skills
- `paste/` — the main one (read latest clipboard, supports SSH or relay)
- `paste-send/` — push file back to laptop (supports relay send)
- `paste-history/` — history list / search / fetch by id or index
- `paste-snippet/` — fetch a named saved snippet
- `recall/` — natural language semantic search over history (Recall v2)

No separate `paste-relay/` (redundant; the above skills document the `--relay` flag where relevant).

See docs/RELAY.md and the individual SKILL.md files.
