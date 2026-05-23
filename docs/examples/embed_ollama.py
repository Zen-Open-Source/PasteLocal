#!/usr/bin/env python3
"""
Recall v2 embedding script for PasteLocal (recommended starter).

Uses Ollama + nomic-embed-text (or any model that returns a vector).

Install:
  - ollama (https://ollama.com)
  - ollama pull nomic-embed-text

Config example in ~/.config/pastelocal/config.toml:

[recall]
enabled = true
timeout_seconds = 30
command = "python3 -u ~/.config/pastelocal/embed_ollama.py"

The daemon pipes the search text (or Vision OCR+desc) to stdin.
We must print a JSON array of floats (or whitespace-separated numbers) to stdout.

This script is robust, retries once on transient errors, and prints dimension on first run.
"""
import json
import os
import sys
import urllib.request
import urllib.error

OLLAMA_URL = os.environ.get("OLLAMA_URL", "http://localhost:11434")
MODEL = os.environ.get("OLLAMA_EMBED_MODEL", "nomic-embed-text")


def embed(text: str) -> list[float]:
    payload = {"model": MODEL, "prompt": text}
    data = json.dumps(payload).encode("utf-8")
    req = urllib.request.Request(
        f"{OLLAMA_URL}/api/embeddings",
        data=data,
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    with urllib.request.urlopen(req, timeout=25) as resp:
        body = json.loads(resp.read())
        vec = body.get("embedding") or body.get("embeddings")
        if isinstance(vec, list) and vec and isinstance(vec[0], (int, float)):
            return [float(x) for x in vec]
        if isinstance(vec, list) and vec and isinstance(vec[0], list):
            return [float(x) for x in vec[0]]
        raise RuntimeError("unexpected response shape from ollama")


def main():
    text = sys.stdin.read()
    if not text.strip():
        print("[]")
        return

    try:
        vec = embed(text)
        # Print compact JSON array (the parser in embedder.go accepts it)
        print(json.dumps(vec, separators=(",", ":")))
    except Exception as e:
        # Fail-open: never crash the daemon. Log to stderr for doctor visibility.
        print(f"embed_ollama.py error: {e}", file=sys.stderr)
        print("[]")


if __name__ == "__main__":
    main()
