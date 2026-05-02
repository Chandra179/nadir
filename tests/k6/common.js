import { SharedArray } from 'k6/data';

export const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';
export const OLLAMA_URL = __ENV.OLLAMA_URL || 'http://localhost:11434';
export const RERANKER_URL = __ENV.RERANKER_URL || 'http://localhost:5002';

// Loaded once in init context, shared across all VUs (no per-VU copy overhead).
// Edit testdata/queries.json to match your knowledge base content.
const queryData = new SharedArray('queries', function () {
  return JSON.parse(open('./testdata/queries.json'));
});

export const QUERIES = queryData[0].general;
export const CACHE_QUERIES = queryData[0].cache_fixed;

export const searchPayload = (query) => JSON.stringify({
  query,
  top_k: 5,
});

export const thresholds = {
  http_req_failed: [{ threshold: 'rate<0.01', abortOnFail: false }],
};
