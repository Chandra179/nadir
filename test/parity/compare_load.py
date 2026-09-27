#!/usr/bin/env python3
"""Apply the rewrite cutover gate to two benchmark_load.py JSON reports."""
import argparse
import json

parser = argparse.ArgumentParser()
parser.add_argument('old')
parser.add_argument('new')
args = parser.parse_args()
old = {w['name']: w for w in json.load(open(args.old))['workloads']}
new = {w['name']: w for w in json.load(open(args.new))['workloads']}
if set(old) != set(new):
    parser.error('workload names differ')
passed = True
for name in old:
    baseline, candidate = old[name], new[name]
    old_p95 = baseline['latency_ms']['p95']
    new_p95 = candidate['latency_ms']['p95']
    if old_p95 is None or new_p95 is None:
        parser.error(f'{name}: missing p95')
    ratio = new_p95 / old_p95
    ok = ratio <= 1.20 and candidate['failures'] <= baseline['failures']
    print(f"{name}: p95 {old_p95:.3f} -> {new_p95:.3f} ms ({(ratio - 1) * 100:+.2f}%), "
          f"failures {baseline['failures']} -> {candidate['failures']}: {'PASS' if ok else 'BLOCK'}")
    passed &= ok
raise SystemExit(0 if passed else 1)
