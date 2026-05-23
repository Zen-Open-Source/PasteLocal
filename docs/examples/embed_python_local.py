#!/usr/bin/env python3
"""
Pure-Python Recall v2 embedder for PasteLocal (fallback when you don't want Ollama).

Uses `sentence-transformers` (all-MiniLM-L6-v2 by default — 384 dimensions, fast, good quality).

Install once:
    pip install sentence-transformers

Config:
    [recall]
    enabled = true
    timeout_seconds = 45
    command = "python3 -u ~/.config/pastelocal/embed_python_local.py"

The script reads text from stdin and writes a JSON array of floats to stdout.
It is intentionally simple and has no external service dependency after the first model download.
"""
import json
import os
import sys

MODEL_NAME = os.environ.get("RECALL_EMBED_MODEL", "all-MiniLM-L6-v2")


def main():
    text = sys.stdin.read().strip()
    if not text:
        print("[]")
        return

    try:
        from sentence_transformers import SentenceTransformer
    except ImportError:
        print(
            "embed_python_local.py: sentence-transformers not installed.\n"
            "Install with: pip install sentence-transformers\n"
            "Then re-run your recall command.",
            file=sys.stderr,
        )
        # Emit a tiny deterministic vector so the daemon doesn't crash on first try
        # (user will see a clear error in doctor / logs).
        print(json.dumps([0.0] * 8))
        return

    # Load model (cached after first run)
    model = SentenceTransformer(MODEL_NAME)
    vec = model.encode(text, normalize_embeddings=True).tolist()
    print(json.dumps(vec, separators=(",", ":")))


if __name__ == "__main__":
    main()
