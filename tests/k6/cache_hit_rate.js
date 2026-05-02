// Fixed query set -> measure semantic cache hit ratio vs QPS gain.
// Queries repeat so cache should warm after first pass.
import http from 'k6/http';
import { check } from 'k6';
import { Counter, Rate, Trend } from 'k6/metrics';
import { BASE_URL, CACHE_QUERIES, thresholds } from './common.js';

const cacheHits = new Counter('cache_hits');
const cacheMisses = new Counter('cache_misses');
const cacheHitRate = new Rate('cache_hit_rate');
const cachedDuration = new Trend('cached_duration', true);
const uncachedDuration = new Trend('uncached_duration', true);

// Fixed small set from testdata/queries.json -> high repeat rate -> cache saturates fast

export const options = {
  vus: 10,
  duration: '90s',
  thresholds: {
    ...thresholds,
    cache_hit_rate: ['rate>0.5'], // expect >50% hits after warmup
  },
};

export default function () {
  const query = CACHE_QUERIES[Math.floor(Math.random() * CACHE_QUERIES.length)];
  const res = http.post(`${BASE_URL}/search`, JSON.stringify({ query, top_k: 5 }), {
    headers: { 'Content-Type': 'application/json' },
  });

  check(res, { 'status 200': (r) => r.status === 200 });

  // Detect cache hit via X-Cache header or response time heuristic (<50ms = cache hit)
  const isHit = res.headers['X-Cache'] === 'HIT' || res.timings.duration < 50;

  if (isHit) {
    cacheHits.add(1);
    cachedDuration.add(res.timings.duration);
  } else {
    cacheMisses.add(1);
    uncachedDuration.add(res.timings.duration);
  }

  cacheHitRate.add(isHit ? 1 : 0);
}
