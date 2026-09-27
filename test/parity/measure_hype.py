#!/usr/bin/env python3
"""Measure one HyPE ingest and retrieval overlap on an isolated Qdrant clone.

Start api-next with HYPE_ENABLED=false or true, the same config and clone
snapshot, then run this script once for each setting. The script writes only
to the configured API and refuses the default live Qdrant port.
"""

import argparse
import json
import threading
import time
import urllib.request
import uuid


def post_json(url, payload, timeout=180):
    body = json.dumps(payload).encode()
    request = urllib.request.Request(url, data=body,
        headers={'Content-Type': 'application/json'}, method='POST')
    with urllib.request.urlopen(request, timeout=timeout) as response:
        return json.load(response)


def upload(url, filename, data):
    boundary = 'nadir-hype-' + uuid.uuid4().hex
    head = (f'--{boundary}\r\n'
        f'Content-Disposition: form-data; name="files"; filename="{filename}"\r\n'
        'Content-Type: text/markdown\r\n\r\n').encode()
    body = head + data + f'\r\n--{boundary}--\r\n'.encode()
    request = urllib.request.Request(url, data=body,
        headers={'Content-Type': f'multipart/form-data; boundary={boundary}'},
        method='POST')
    with urllib.request.urlopen(request, timeout=240) as response:
        return json.load(response)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--api', default='http://127.0.0.1:8100')
    parser.add_argument('--qdrant', default='http://127.0.0.1:16333')
    parser.add_argument('--mode', choices=['off', 'on'], required=True)
    parser.add_argument('--json-out', required=True)
    args = parser.parse_args()
    api = args.api.rstrip('/')
    qdrant = args.qdrant.rstrip('/')
    if qdrant in ('http://127.0.0.1:6333', 'http://localhost:6333'):
        parser.error('refusing the default live Qdrant port')
    filename = 'hype-measure.md'
    count_url = qdrant + '/collections/documents_chunks__active/points/count'
    count_filter = {'filter': {'must': [{'key': 'file_path',
        'match': {'value': filename}}]}, 'exact': True}
    def count():
        return post_json(count_url, count_filter)['result']['count']
    before = count()
    paragraph = ('A derivative measures how a function changes. The secant line '
        'uses two points to estimate a slope. A limiting secant gives the tangent.\n\n')
    content = '# HyPE measurement\n\n' + paragraph * 16
    query_url = api + '/api/v1/turns'
    def query(index):
        started = time.monotonic()
        try:
            body = post_json(query_url, {'query': f'secant derivative measurement {index}',
                'generate': False, 'top_k': 5})
            error = body.get('error')
        except Exception as exc:
            error = str(exc)
        ended = time.monotonic()
        return {'start': started, 'end': ended, 'latency_ms': round((ended-started)*1000, 3),
                'error': error}
    baseline = [query(-i-1) for i in range(2)]
    begin = threading.Event()
    done = threading.Event()
    concurrent = []
    def query_loop():
        begin.wait()
        index = 0
        while not done.is_set() and index < 16:
            concurrent.append(query(index))
            index += 1
    worker = threading.Thread(target=query_loop)
    worker.start()
    begin.set()
    ingest_start = time.monotonic()
    try:
        result = upload(api + '/api/v1/documents', filename, content.encode())
    finally:
        ingest_end = time.monotonic()
        done.set()
        worker.join()
    after = count()
    overlap = [s for s in concurrent if s['start'] < ingest_end and s['end'] > ingest_start]
    report = {'mode': args.mode, 'document_bytes': len(content.encode()),
        'ingest_ms': round((ingest_end-ingest_start)*1000, 3),
        'ingest_result': result, 'points_before': before, 'points_after': after,
        'point_growth': after-before,
        'idle_query_ms': [s['latency_ms'] for s in baseline],
        'concurrent_query_ms': [s['latency_ms'] for s in overlap],
        'concurrent_query_errors': [s['error'] for s in overlap if s['error']],
        'overlap_queries': len(overlap)}
    with open(args.json_out, 'w', encoding='utf-8') as f:
        json.dump(report, f, indent=2, sort_keys=True)
        f.write('\n')
    print(json.dumps(report, indent=2, sort_keys=True))


if __name__ == '__main__':
    main()
