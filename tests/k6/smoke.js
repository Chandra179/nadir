import http from 'k6/http';
import { check, sleep } from 'k6';
import { Trend } from 'k6/metrics';
import { BASE_URL, QUERIES, searchPayload, thresholds } from './common.js';

const searchDuration = new Trend('search_duration', true);

export const options = {
  vus: 1,
  duration: '30s',
  thresholds: {
    ...thresholds,
    http_req_duration: ['p(50)<500', 'p(95)<2000'],
  },
};

export default function () {
  const query = QUERIES[Math.floor(Math.random() * QUERIES.length)];
  const res = http.post(`${BASE_URL}/search`, searchPayload(query), {
    headers: { 'Content-Type': 'application/json' },
  });

  searchDuration.add(res.timings.duration);

  check(res, {
    'status 200': (r) => r.status === 200,
    'has results': (r) => {
      try {
        const body = JSON.parse(r.body);
        return Array.isArray(body.results) && body.results.length > 0;
      } catch {
        return false;
      }
    },
  });

  sleep(1);
}
