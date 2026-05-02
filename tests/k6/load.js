import http from 'k6/http';
import { check, sleep } from 'k6';
import { Rate, Trend } from 'k6/metrics';
import { BASE_URL, QUERIES, searchPayload, thresholds } from './common.js';

const errorRate = new Rate('error_rate');
const searchDuration = new Trend('search_duration', true);

export const options = {
  stages: [
    { duration: '1m', target: 10 },
    { duration: '2m', target: 25 },
    { duration: '2m', target: 50 },
    { duration: '1m', target: 0 },
  ],
  thresholds: {
    ...thresholds,
    http_req_duration: ['p(95)<5000'],
    error_rate: ['rate<0.05'],
  },
};

export default function () {
  const query = QUERIES[Math.floor(Math.random() * QUERIES.length)];
  const res = http.post(`${BASE_URL}/search`, searchPayload(query), {
    headers: { 'Content-Type': 'application/json' },
    timeout: '30s',
  });

  searchDuration.add(res.timings.duration);
  errorRate.add(res.status !== 200);

  check(res, {
    'status 200': (r) => r.status === 200,
  });

  sleep(0.5);
}
