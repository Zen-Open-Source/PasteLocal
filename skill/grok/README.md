# Grok-Native PasteLocal Skills

These are first-class Grok skills (SKILL.md + optional helpers) for using PasteLocal from inside Grok sessions on remote machines.

## Installation (manual for v1)
Copy the subdirectories into your Grok skills folder:

```bash
cp -r skill/grok/paste ~/.grok/skills/pastelocal-paste
cp -r skill/grok/paste-send ~/.grok/skills/pastelocal-paste-send
# ... repeat for history + relay variants
```

Then just say `/paste` or `/pastelocal-paste` (Grok will discover them).

A future `pastelocal install-grok-skills` command will automate this + register them.

## Available Skills (to be completed by implementer)
- `paste/` — the main one (read latest clipboard)
- `paste-send/` — push file back to laptop
- `paste-history/`
- `paste-snippet/`
- `paste-relay/` (explicit relay variants)

See the main [RELAY_V1_IMPLEMENTATION_SPEC.md](../docs/RELAY_V1_IMPLEMENTATION_SPEC.md) for the full plan.
