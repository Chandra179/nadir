// End-to-end smoke test for the browser runtime, run under Node. It drives the
// real versioned JSON/SSE API through nadir's in-page fetch bridge, with
// scripted stand-ins for the embedding model, LLM, reranker and PDF extractor.
// usage: node smoke.mjs <wasm_exec.js> <nadir.wasm>
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { pathToFileURL } from 'node:url';

const [wasmExec, wasmFile] = process.argv.slice(2);
await import(pathToFileURL(wasmExec));
const go = new Go();
const { instance } = await WebAssembly.instantiate(await readFile(wasmFile), go.importObject);
go.run(instance); // never settles: main parks on select{}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

// ---- scripted models -------------------------------------------------------
const DIMS = 256;
function hash(word) {
  let h = 2166136261;
  for (const c of word) h = Math.imul(h ^ c.codePointAt(0), 16777619) >>> 0;
  return h;
}
function embedText(text) {
  const v = new Float32Array(DIMS);
  for (const word of text.toLowerCase().split(/[^a-z0-9]+/).filter(Boolean)) v[hash(word) % DIMS]++;
  const norm = Math.hypot(...v) || 1;
  return v.map((x) => x / norm);
}
const calls = { batch: 0, generate: 0, complete: 0, rerank: 0, convert: 0 };
let generatorMode = 'fast';
const models = {
  dimensions: DIMS,
  embed: async (text) => embedText(text),
  embedBatch: async (texts) => { calls.batch++; return texts.map(embedText); },
  generate: async (prompt, { signal, emit }) => {
    calls.generate++;
    const tokens = generatorMode === 'slow' ? Array(50).fill('word ') : ['Rockets ', 'burn ', 'fuel.'];
    for (const token of tokens) {
      if (signal.aborted) throw new Error('aborted');
      emit(token);
      await sleep(generatorMode === 'slow' ? 20 : 1);
    }
  },
  complete: async () => { calls.complete++; return 'Standalone search query: How do rockets reach orbit?'; },
  rerank: async (query, passages) => { calls.rerank++; return passages.map((_, i) => passages.length - i); },
  convert: async (name) => { calls.convert++; return `# Savannah\n\nZebras roam the savannah near ${name}.`; },
};

// ---- in-page fetch ---------------------------------------------------------
async function api(rt, method, url, { json, body, headers = {}, signal } = {}) {
  if (json !== undefined) { body = JSON.stringify(json); headers = { 'Content-Type': 'application/json', ...headers }; }
  const chunks = [];
  let status, responseHeaders;
  await rt.fetch({
    method, url, headers, body, signal,
    onHead: (s, h) => { status = s; responseHeaders = h; },
    onChunk: (c) => chunks.push(Buffer.from(c)),
  });
  const text = Buffer.concat(chunks).toString('utf8');
  return { status, headers: responseHeaders, text, json: () => JSON.parse(text) };
}
async function upload(rt, files) {
  const form = new FormData();
  for (const [name, content] of files) form.append('files', new File([content], name));
  const encoded = new Response(form);
  return api(rt, 'POST', '/api/v1/documents', {
    body: new Uint8Array(await encoded.arrayBuffer()),
    headers: { 'Content-Type': encoded.headers.get('content-type') },
  });
}
function parseSSE(text) {
  return text.split('\n\n').filter(Boolean).map((block) => {
    const event = { data: [] };
    for (const line of block.split('\n')) {
      if (line.startsWith('id: ')) event.id = Number(line.slice(4));
      else if (line.startsWith('event: ')) event.event = line.slice(7);
      else if (line.startsWith('data: ')) event.data.push(line.slice(6));
    }
    return { ...event, data: event.data.join('\n') };
  });
}

// ---- scenario --------------------------------------------------------------
const rt = await nadir.start(models);

let res = await api(rt, 'GET', '/api/v1/health');
assert.equal(res.status, 200);
res = await api(rt, 'GET', '/api/v1/ready');
assert.equal(res.status, 200, res.text);
const ready = res.json();
assert.equal(ready.ready, true);
assert.equal(ready.checks.generator.ready, true);

res = await upload(rt, [
  ['pets.md', '# Pets\n\nCats purr and sleep all day in the sun.\n'],
  ['space.md', '# Space\n\nRockets burn fuel to reach orbit around the planet.\n'],
  ['field.pdf', '%PDF-1.4 not really'],
]);
assert.equal(res.status, 200, res.text);
const ingested = res.json();
assert.equal(ingested.failed, 0, res.text);
assert.equal(ingested.processed, 3, res.text);
assert.equal(calls.convert, 1, 'pdf should go through the host converter');

// Retrieval-only turn.
res = await api(rt, 'POST', '/api/v1/turns', { json: { query: 'zebras savannah', top_k: 1, generate: false } });
assert.equal(res.status, 200, res.text);
assert.equal(res.json().results[0].file_path, 'field.pdf');

// Streaming generation over SSE.
res = await api(rt, 'POST', '/api/v1/turns', { json: { query: 'rockets orbit planet', top_k: 2, generate: true } });
const turn = res.json();
assert.ok(turn.turn_id && turn.stream_url && turn.session_id, res.text);
assert.equal(turn.results[0].file_path, 'space.md');
res = await api(rt, 'GET', turn.stream_url);
assert.equal(res.status, 200);
assert.match(res.headers['Content-Type'], /text\/event-stream/);
const events = parseSSE(res.text);
assert.deepEqual(events.filter((e) => e.event === 'token').map((e) => e.data), ['Rockets ', 'burn ', 'fuel.']);
assert.equal(events.at(-1).event, 'done');
// Reconnecting with Last-Event-ID replays only what was missed.
res = await api(rt, 'GET', turn.stream_url, { headers: { 'Last-Event-ID': String(events[0].id) } });
assert.deepEqual(parseSSE(res.text).filter((e) => e.event === 'token').map((e) => e.data), ['burn ', 'fuel.']);

// The persisted session carries the answer.
res = await api(rt, 'GET', `/api/v1/sessions/${turn.session_id}`);
assert.equal(res.status, 200, res.text);
assert.match(JSON.stringify(res.json()), /Rockets burn fuel\./);

// A follow-up in the same session is rewritten by the host LLM before retrieval.
res = await api(rt, 'POST', '/api/v1/turns', {
  json: { query: 'how fast does it go?', session_id: turn.session_id, generate: false, top_k: 1 },
});
assert.equal(res.json().rewritten_query, 'How do rockets reach orbit?');
assert.equal(calls.complete, 1);

// Cancel keeps the partial answer.
generatorMode = 'slow';
res = await api(rt, 'POST', '/api/v1/turns', { json: { query: 'rockets orbit', generate: true, top_k: 1 } });
const slow = res.json();
const streaming = api(rt, 'GET', slow.stream_url);
await sleep(150);
res = await api(rt, 'POST', `/api/v1/turns/${slow.turn_id}/cancel`);
assert.equal(res.status, 204);
const cancelled = await streaming;
assert.ok(parseSSE(cancelled.text).some((e) => e.event === 'token'), 'tokens before cancel');
generatorMode = 'fast';

// Editing the first turn truncates it and its tail.
res = await api(rt, 'POST', '/api/v1/turns', {
  json: { query: 'cats sun', session_id: turn.session_id, edit: true, edit_sequence: 0, generate: false, top_k: 1 },
});
assert.equal(res.status, 200, res.text);
res = await api(rt, 'GET', `/api/v1/sessions/${turn.session_id}`);
assert.equal(JSON.stringify(res.json()).includes('Rockets burn fuel.'), false, 'edited turn replaced');

// Aborting a request cancels its context.
const controller = new AbortController();
controller.abort();
await assert.rejects(api(rt, 'GET', '/api/v1/health', { signal: controller.signal }), /context canceled|canceled/i);

// Persist everything, restart, and find it again.
const snapshot = await rt.snapshot();
assert.ok(snapshot.documents instanceof Uint8Array && snapshot.history instanceof Uint8Array);
await rt.close();
const restored = await nadir.start({ ...models, snapshot });
res = await api(restored, 'GET', '/api/v1/sessions');
assert.ok(res.json().sessions.length >= 1, res.text);
res = await api(restored, 'POST', '/api/v1/turns', { json: { query: 'cats purr sun', top_k: 1, generate: false } });
assert.equal(res.json().results[0].file_path, 'pets.md');

// Deleting history and resetting documents.
res = await api(restored, 'DELETE', '/api/v1/sessions');
assert.ok(res.status === 200 || res.status === 204, res.text);
res = await api(restored, 'GET', '/api/v1/sessions');
assert.equal(res.json().sessions.length, 0);
res = await api(restored, 'POST', '/api/v1/documents/reset');
assert.equal(res.status, 200, res.text);
assert.deepEqual(await restored.versions(), {});

// Host failures surface as errors, bad options fail fast.
const broken = await nadir.start({ ...models, embed: async () => { throw new Error('model offline'); }, embedBatch: undefined });
res = await api(broken, 'POST', '/api/v1/turns', { json: { query: 'anything', generate: false } });
assert.match(res.json().error, /Search failed/);
assert.deepEqual(res.json().results, []);
await assert.rejects(nadir.start({ dimensions: 0, embed: models.embed }), /dimensions/);

console.log('ok: api health/ready, upload+pdf, retrieval, sse stream+replay, sessions, rewrite, cancel, edit, abort, snapshot/restore, reset', JSON.stringify(calls));
process.exit(0);
