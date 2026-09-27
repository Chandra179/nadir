#!/usr/bin/env python3
"""Deterministic local Ollama wire stub for old/new backend overhead checks."""
import json
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

VECTOR = [1.0] + [0.0] * 767

class Handler(BaseHTTPRequestHandler):
    def log_message(self, *_args):
        pass

    def send_json(self, value):
        raw = json.dumps(value).encode()
        self.send_response(200)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Content-Length', str(len(raw)))
        self.end_headers()
        self.wfile.write(raw)

    def do_GET(self):
        if self.path == '/api/ps':
            self.send_json({'models': [{'name': 'nomic-embed-text:latest'}]})
        else:
            self.send_error(404)

    def do_POST(self):
        length = int(self.headers.get('Content-Length', '0'))
        body = json.loads(self.rfile.read(length) or b'{}')
        if self.path == '/api/embed':
            inputs = body.get('input', [])
            if isinstance(inputs, str): inputs = [inputs]
            self.send_json({'embeddings': [VECTOR for _ in inputs]})
        elif self.path == '/api/chat':
            if body.get('stream'):
                raw = b'{"message":{"content":"fixed answer"}}\n{"done":true}\n'
                self.send_response(200)
                self.send_header('Content-Type', 'application/x-ndjson')
                self.send_header('Content-Length', str(len(raw)))
                self.end_headers()
                self.wfile.write(raw)
            else:
                self.send_json({'message': {'content': 'fixed query'}})
        else:
            self.send_error(404)

if __name__ == '__main__':
    ThreadingHTTPServer(('127.0.0.1', 11435), Handler).serve_forever()
