#!/usr/bin/env python3
"""Capture the public API shape without persisting document or answer text."""
import argparse
import json
import urllib.error
import urllib.request


def call(base, method, path, payload=None, headers=None):
    data = None if payload is None else json.dumps(payload).encode()
    req = urllib.request.Request(base + path, data=data, method=method,
        headers={**({'Content-Type': 'application/json'} if data is not None else {}), **(headers or {})})
    try:
        with urllib.request.urlopen(req, timeout=180) as resp:
            return resp.status, resp.headers.get('Content-Type', ''), resp.read()
    except urllib.error.HTTPError as resp:
        return resp.status, resp.headers.get('Content-Type', ''), resp.read()


def shape(value):
    if isinstance(value, dict):
        return {key: shape(value[key]) for key in sorted(value)}
    if isinstance(value, list):
        return {'type': 'array', 'item': shape(value[0]) if value else None}
    return type(value).__name__


def capture(base):
    result = {}
    for name, method, path, body in [
        ('health', 'GET', '/api/v1/health', None),
        ('ready', 'GET', '/api/v1/ready', None),
        ('malformed_turn', 'POST', '/api/v1/turns', {'unexpected': 1}),
        ('missing_events', 'GET', '/api/v1/turns/missing/events', None),
        ('missing_cancel', 'POST', '/api/v1/turns/missing/cancel', None),
        ('wrong_method', 'GET', '/api/v1/documents/reset', None),
        ('sessions', 'GET', '/api/v1/sessions', None),
        ('retrieval', 'POST', '/api/v1/turns', {'query': 'secant formula', 'generate': False}),
    ]:
        status, content_type, raw = call(base, method, path, body)
        entry = {'status': status, 'content_type': content_type}
        if raw:
            try:
                entry['shape'] = shape(json.loads(raw))
            except ValueError:
                entry['body'] = raw.decode(errors='replace')
        result[name] = entry
    status, content_type, raw = call(base, 'POST', '/api/v1/turns', {'query': 'What is the secant formula?', 'generate': True})
    turn = json.loads(raw)
    result['generated_turn'] = {'status': status, 'content_type': content_type, 'shape': shape(turn)}
    if turn.get('stream_url'):
        status, content_type, raw = call(base, 'GET', turn['stream_url'])
        events = []
        ids = []
        for line in raw.decode(errors='replace').splitlines():
            if line.startswith('event: '): events.append(line[7:])
            if line.startswith('id: '):
                try: ids.append(int(line[4:]))
                except ValueError: pass
        result['stream'] = {'status': status, 'content_type': content_type,
            'event_kinds': sorted(set(events)), 'ids_strictly_increasing': all(b > a for a, b in zip(ids, ids[1:])),
            'has_ids': bool(ids), 'terminal_event': events[-1] if events else None}
        status, content_type, raw = call(base, 'GET', turn['stream_url'], headers={'Last-Event-ID': str(ids[-1]) if ids else '0'})
        result['replay_after_terminal'] = {'status': status, 'content_type': content_type, 'body': raw.decode(errors='replace')}
    return result


if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('--base-url', default='http://127.0.0.1:8100')
    parser.add_argument('--out', required=True)
    args = parser.parse_args()
    with open(args.out, 'w') as out:
        json.dump(capture(args.base_url.rstrip('/')), out, indent=2, sort_keys=True)
        out.write('\n')
