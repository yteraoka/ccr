#!/usr/bin/env -S uv run --script
# /// script
# requires-python = ">=3.10"
# dependencies = [
#     "sentence-transformers>=3.4.1",
#     "transformers>=4.48.0",
#     "sentencepiece",
# ]
# ///
"""Serve Ruri v3 on an OpenAI-compatible embeddings endpoint for ccr.

ccr's vector search (`ccr search`) talks to any server speaking the OpenAI
embeddings API. Ruri v3 (https://huggingface.co/collections/cl-nagoya/ruri-v3),
a Japanese embedding model, is published for sentence-transformers rather
than for Ollama or llama.cpp, so this serves it that way:

    uv run scripts/ruri-embedding-server.py

and listens on http://127.0.0.1:8080/v1/embeddings, which is where ccr looks
when the config file's history.embedding section is empty. The model is
downloaded from Hugging Face on first start.

The server adds no prefixes: ccr itself prepends Ruri v3's "検索クエリ: " /
"検索文書: " to what it sends.
"""

import argparse
import json
import sys
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

MAX_BODY = 64 << 20


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    parser.add_argument("--model", default="cl-nagoya/ruri-v3-310m",
                        help="model to serve (ruri-v3-30m / 70m / 130m / 310m)")
    parser.add_argument("--host", default="127.0.0.1")
    parser.add_argument("--port", type=int, default=8080)
    parser.add_argument("--device", default=None, help="cpu, cuda, mps... (default: best available)")
    parser.add_argument("--batch-size", type=int, default=8)
    parser.add_argument("--verbose", action="store_true", help="log every request")
    args = parser.parse_args()

    from sentence_transformers import SentenceTransformer

    print(f"loading {args.model}...", file=sys.stderr)
    model = SentenceTransformer(args.model, device=args.device)
    # one request at a time: the model is not safe to call concurrently,
    # and on a laptop parallel requests would only fight over the CPU/GPU
    lock = threading.Lock()

    class Handler(BaseHTTPRequestHandler):
        def do_POST(self) -> None:
            if self.path.rstrip("/") not in ("/v1/embeddings", "/embeddings"):
                self.reply(404, {"error": {"message": f"no such endpoint: {self.path}"}})
                return
            try:
                length = int(self.headers.get("Content-Length", "0"))
                if length <= 0 or length > MAX_BODY:
                    raise ValueError("missing or oversized request body")
                req = json.loads(self.rfile.read(length))
                inputs = req.get("input")
                if isinstance(inputs, str):
                    inputs = [inputs]
                if not isinstance(inputs, list) or not inputs or not all(isinstance(t, str) for t in inputs):
                    raise ValueError('"input" must be a string or a non-empty list of strings')
            except (ValueError, AttributeError) as e:
                self.reply(400, {"error": {"message": str(e)}})
                return

            with lock:
                vectors = model.encode(inputs, batch_size=args.batch_size, convert_to_numpy=True)
            self.reply(200, {
                "object": "list",
                "model": args.model,
                "data": [
                    {"object": "embedding", "index": i, "embedding": v.tolist()}
                    for i, v in enumerate(vectors)
                ],
                "usage": {"prompt_tokens": 0, "total_tokens": 0},
            })

        def reply(self, status: int, body: dict) -> None:
            data = json.dumps(body).encode()
            self.send_response(status)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(data)))
            self.end_headers()
            self.wfile.write(data)

        def log_message(self, fmt: str, *a) -> None:
            if args.verbose:
                super().log_message(fmt, *a)

    server = ThreadingHTTPServer((args.host, args.port), Handler)
    print(f"serving {args.model} at http://{args.host}:{args.port}/v1/embeddings", file=sys.stderr)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass


if __name__ == "__main__":
    main()
