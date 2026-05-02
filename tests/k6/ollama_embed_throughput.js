// Hammer Ollama embed endpoint directly to find serial/parallel capacity.
import http from 'k6/http';
import { check } from 'k6';
import { Trend, Rate } from 'k6/metrics';
import { OLLAMA_URL, QUERIES, thresholds } from './common.js';

const embedDuration = new Trend('embed_duration', true);
const errorRate = new Rate('embed_error_rate');

export const options = {
  stages: [
    { duration: '30s', target: 1 },   // serial baseline
    { duration: '30s', target: 4 },   // light parallel
    { duration: '30s', target: 8 },   // find saturation
    { duration: '30s', target: 1 },   // cool down
  ],
  thresholds: {
    ...thresholds,
    embed_error_rate: ['rate<0.01'],
  },
};

export default function () {
  const text = QUERIES[Math.floor(Math.random() * QUERIES.length)];
  const res = http.post(`${OLLAMA_URL}/api/embeddings`, JSON.stringify({
    model: 'nomic-embed-text',
    prompt: text,
  }), {
    headers: { 'Content-Type': 'application/json' },
    timeout: '30s',
  });

  embedDuration.add(res.timings.duration);
  errorRate.add(res.status !== 200);

  check(res, {
    'status 200': (r) => r.status === 200,
    'has embedding': (r) => {
      try {
        const body = JSON.parse(r.body);
        return Array.isArray(body.embedding) && body.embedding.length > 0;
      } catch {
        return false;
      }
    },
  });
}
