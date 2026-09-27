#!/usr/bin/env python3
"""Add one inactive legacy chunk to an isolated Qdrant parity clone."""
import argparse
import hashlib
import json
import urllib.request
import uuid

parser = argparse.ArgumentParser()
parser.add_argument('--base-url', default='http://127.0.0.1:16333')
args = parser.parse_args()
base = args.base_url.rstrip('/')
if base in ('http://127.0.0.1:6333', 'http://localhost:6333'):
    parser.error('refusing to seed the default live Qdrant port')
name = 'documents_chunks__active'
request = urllib.request.Request(base + f'/collections/{name}/points/scroll',
    data=b'{"limit":1,"with_payload":true,"with_vector":true}',
    headers={'Content-Type': 'application/json'}, method='POST')
point = json.load(urllib.request.urlopen(request))['result']['points'][0]
payload = point['payload'].copy()
payload['active'] = False
payload['source_sha'] = hashlib.sha256(b'nadir parity retired version').hexdigest()
key = f"{payload['file_path']}:{payload['source_sha']}:{payload['line_start']}:{payload['chunk_index']}"
point['id'] = str(uuid.uuid5(uuid.UUID('a3b4c5d6-e7f8-4a5b-9c0d-1e2f3a4b5c6d'), key))
point['payload'] = payload
body = json.dumps({'points': [point]}).encode()
request = urllib.request.Request(base + f'/collections/{name}/points?wait=true',
    data=body, headers={'Content-Type': 'application/json'}, method='PUT')
response = json.load(urllib.request.urlopen(request))
if response['status'] != 'ok':
    raise RuntimeError(response)
print(point['id'])
