// Keyword-only search to isolate Qdrant BM25 throughput (no dense embed, no reranker).
// Requires server supports ?mode=keyword or sparse-only endpoint.
// If not, this script hits /search with a flag and measures baseline vs hybrid delta.
import http from 'k6/http';
import { check, sleep } from 'k6';
import { Trend } from 'k6/metrics';
import { BASE_URL, QUERIES, thresholds } from './common.js';

const keywordDuration = new Trend('keyword_duration', true);
const hybridDuration = new Trend('hybrid_duration', true);

export const options = {
  vus: 5,
  duration: '60s',
  thresholds: {
    ...thresholds,
    keyword_duration: ['p(95)<1000'],
  },
};

export default function () {
  const query = QUERIES[Math.floor(Math.random() * QUERIES.length)];

  // keyword-only: no hyde, no reranker, sparse only
  const kwRes = http.post(`${BASE_URL}/search`, JSON.stringify({
    query,
    top_k: 5,
    hyde: false,
    rerank: false,
    sparse_only: true,
  }), { headers: { 'Content-Type': 'application/json' } });

  keywordDuration.add(kwRes.timings.duration);
  check(kwRes, { 'keyword 200': (r) => r.status === 200 });

  // hybrid for delta comparison
  const hybridRes = http.post(`${BASE_URL}/search`, JSON.stringify({
    query,
    top_k: 5,
    hyde: false,
    rerank: false,
  }), { headers: { 'Content-Type': 'application/json' } });

  hybridDuration.add(hybridRes.timings.duration);
  check(hybridRes, { 'hybrid 200': (r) => r.status === 200 });

  sleep(0.5);
}
