import type { Models, Progress } from "./types";

const DIMS = 256;

function hash(word: string): number {
  let h = 2166136261;
  for (let i = 0; i < word.length; i++) h = Math.imul(h ^ word.charCodeAt(i), 16777619) >>> 0;
  return h;
}

/** Bag-of-words vector: texts sharing words are close. Stands in for a model. */
export function embedText(text: string): Float32Array {
  const v = new Float32Array(DIMS);
  for (const word of text.toLowerCase().split(/[^a-z0-9]+/).filter(Boolean)) v[hash(word) % DIMS]++;
  const norm = Math.hypot(...v) || 1;
  return v.map((x) => x / norm);
}

const sleep = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

/** Deterministic models for tests and offline demos: no downloads, no GPU. */
export class MockModels implements Models {
  readonly dimensions = DIMS;
  readonly embeddingModel = "mock-bag-of-words";
  readonly generatorModel = "mock-llm";

  async warmup(onProgress: (p: Progress) => void): Promise<void> {
    onProgress({ stage: "embedding", label: "Mock embedding model", fraction: 1 });
  }

  async embed(texts: string[]): Promise<Float32Array[]> {
    return texts.map(embedText);
  }

  async rerank(query: string, passages: string[]): Promise<number[]> {
    const terms = new Set(query.toLowerCase().split(/[^a-z0-9]+/).filter(Boolean));
    return passages.map((p) => p.toLowerCase().split(/[^a-z0-9]+/).filter((w) => terms.has(w)).length);
  }

  async generate(prompt: string, signal: AbortSignal, emit: (token: string) => void): Promise<void> {
    // Answer from the first context sentence so tests can check grounding.
    const context = prompt.split(/\n/).find((line) => /\w{4,}/.test(line) && !/^(question|answer|context|\[)/i.test(line)) ?? "";
    const words = `Mock answer: ${context}`.split(/\s+/).slice(0, 24);
    for (const word of words) {
      if (signal.aborted) throw new DOMException("aborted", "AbortError");
      emit(`${word} `);
      await sleep(2);
    }
  }

  async complete(_system: string, user: string): Promise<string> {
    // Echo the last user question so follow-ups remain searchable.
    const questions = [...user.matchAll(/User: (.+)/g)].map((m) => m[1]);
    return questions.at(-1) ?? user;
  }
}
