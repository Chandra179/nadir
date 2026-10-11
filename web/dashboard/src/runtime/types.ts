/** Shared contracts for the in-browser runtime. */

export type Progress = {
  /** Which model is loading, for example "embedding" or "llm". */
  stage: string;
  label: string;
  /** 0..1 when known. */
  fraction?: number;
};

/** The model capabilities the WASM core calls out to. */
export interface Models {
  readonly dimensions: number;
  readonly embeddingModel: string;
  readonly generatorModel?: string;
  /** Loads weights, reporting progress. Resolves when ready to serve. */
  warmup(onProgress: (p: Progress) => void): Promise<void>;
  embed(texts: string[]): Promise<Float32Array[]>;
  rerank?(query: string, passages: string[]): Promise<number[]>;
  /** Streams answer tokens through emit; must stop when signal aborts. */
  generate?(prompt: string, signal: AbortSignal, emit: (token: string) => void): Promise<void>;
  /** A short deterministic completion, used for follow-up query rewriting. */
  complete?(system: string, user: string): Promise<string>;
}

export type MockModelsConfig = { kind: "mock" };

export type WebModelsConfig = {
  kind: "web";
  embeddingModel: string;
  embeddingDimensions: number;
  /** "q8" keeps EmbeddingGemma near 300 MB. */
  embeddingDtype: string;
  /** Where onnxruntime-web's .wasm/.mjs files are served from. transformers.js
   *  defaults to jsDelivr; set this to self-host (R2, same origin). The wasm is
   *  ~27 MB, over Cloudflare's 25 MiB static-asset limit, so it cannot ship in
   *  the Worker's own assets. */
  ortBaseURL?: string;
  /** ONNX execution backend; "wasm" (CPU) is the predictable default. */
  device?: "wasm" | "webgpu";
  rerankerModel?: string;
  /** A WebLLM model id; omit for retrieval-only mode. */
  llmModel?: string;
  contextWindow?: number;
  maxOutputTokens?: number;
};

export type RuntimeConfig = {
  wasmURL: string;
  wasmExecURL: string;
  models: MockModelsConfig | WebModelsConfig;
  /** Persist documents and chat history to the origin-private file system. */
  persist: boolean;
  /** Namespaces the stored snapshot. */
  storageName: string;
};

export const DEFAULT_WEB_MODELS: WebModelsConfig = {
  kind: "web",
  embeddingModel: "onnx-community/embeddinggemma-300m-ONNX",
  embeddingDimensions: 768,
  embeddingDtype: "q8",
  rerankerModel: "Xenova/ms-marco-MiniLM-L-6-v2",
  llmModel: "Qwen2.5-1.5B-Instruct-q4f16_1-MLC",
  contextWindow: 4096,
  maxOutputTokens: 512,
};
