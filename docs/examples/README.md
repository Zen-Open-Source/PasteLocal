# Recall v2 – Example Embedding Scripts

These scripts turn any local embedding model into a first-class semantic search backend for PasteLocal history (`pastelocal-remote --search "..."` and the `/recall` skill).

## Recommended: Ollama + nomic-embed-text (fastest to start)

1. Install Ollama: https://ollama.com
2. `ollama pull nomic-embed-text`
3. Copy `embed_ollama.py` to `~/.config/pastelocal/embed_ollama.py` and make it executable.
4. Add to your `~/.config/pastelocal/config.toml`:

```toml
[recall]
enabled = true
timeout_seconds = 30
command = "python3 -u ~/.config/pastelocal/embed_ollama.py"
```

5. Restart the daemon (`pastelocal restart` or send SIGHUP).

## Pure Python fallback (no external service)

Use `embed_python_local.py` if you prefer everything running inside one Python process.

```bash
pip install sentence-transformers
cp docs/examples/embed_python_local.py ~/.config/pastelocal/
```

Config:

```toml
[recall]
enabled = true
timeout_seconds = 60
command = "python3 -u ~/.config/pastelocal/embed_python_local.py"
```

The first run will download the ~80 MB model; subsequent runs are fast.

## How it works

- PasteLocal pipes the text to be embedded (plain text or the concatenated OCR + description from VisionPaste) to the command’s **stdin**.
- The script must print a JSON array of floats (or whitespace-separated numbers) to **stdout**.
- The daemon never blocks on embedding failures (fail-open).
- Concealed items (password manager popups) are never sent to any embedder.

## Verifying it works

```bash
pastelocal doctor
# Look for the Recall section – you should see green checks and a dimension.

pastelocal-remote --search "the red error dialog" --limit 5
```

Happy semantic searching!
