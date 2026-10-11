/** Embedding and reranking with transformers.js (ONNX Runtime Web). Runs in its
 *  own worker so inference never blocks the WASM core or the UI. */
import {
  AutoModelForSequenceClassification,
  AutoTokenizer,
  env,
  pipeline,
  type FeatureExtractionPipeline,
  type PreTrainedModel,
  type PreTrainedTokenizer,
  type ProgressInfo,
} from "@huggingface/transformers";
import { serve, type Endpoint, type Handlers } from "./rpc";

type Scope = { onmessage: ((event: MessageEvent) => void) | null; postMessage(m: unknown, t?: Transferable[]): void };
const scope = self as unknown as Scope;

env.allowLocalModels = false;

let extractor: FeatureExtractionPipeline | undefined;
let tokenizer: PreTrainedTokenizer | undefined;
let classifier: PreTrainedModel | undefined;

type LoadConfig = { embeddingModel: string; dtype: string; device: "wasm" | "webgpu"; rerankerModel?: string; ortBaseURL?: string };

function progressOf(stage: string, label: string, emit: (n: string, v: unknown) => void) {
  const seen = new Map<string, { loaded: number; total: number }>();
  return (info: ProgressInfo) => {
    if (info.status === "progress" && "file" in info) {
      seen.set(info.file, { loaded: info.loaded, total: info.total });
      let loaded = 0;
      let total = 0;
      for (const f of seen.values()) {
        loaded += f.loaded;
        total += f.total;
      }
      emit("progress", { stage, label, fraction: total ? loaded / total : undefined });
    }
  };
}

const handlers: Handlers = {
  async load(ctx, config: LoadConfig) {
    const emit = (n: string, v: unknown) => ctx.emit(n, v);
    if (config.ortBaseURL && env.backends.onnx.wasm) env.backends.onnx.wasm.wasmPaths = config.ortBaseURL;
    extractor = (await pipeline("feature-extraction", config.embeddingModel, {
      dtype: config.dtype as never,
      device: config.device,
      progress_callback: progressOf("embedding", config.embeddingModel, emit),
    })) as FeatureExtractionPipeline;
    if (config.rerankerModel) {
      tokenizer = await AutoTokenizer.from_pretrained(config.rerankerModel, {
        progress_callback: progressOf("reranker", config.rerankerModel, emit),
      });
      classifier = await AutoModelForSequenceClassification.from_pretrained(config.rerankerModel, {
        dtype: "q8",
        device: config.device,
        progress_callback: progressOf("reranker", config.rerankerModel, emit),
      });
    }
    return true;
  },

  async embed(_ctx, texts: string[]) {
    if (!extractor) throw new Error("embedding model not loaded");
    const output = await extractor(texts, { pooling: "mean", normalize: true });
    const [rows, width] = output.dims as [number, number];
    const data = output.data as Float32Array;
    const vectors: Float32Array[] = [];
    for (let i = 0; i < rows; i++) vectors.push(data.slice(i * width, (i + 1) * width));
    return vectors;
  },

  async rerank(_ctx, query: string, passages: string[]) {
    if (!tokenizer || !classifier) throw new Error("reranker not loaded");
    const inputs = tokenizer(new Array(passages.length).fill(query), {
      text_pair: passages,
      padding: true,
      truncation: true,
      max_length: 512,
    });
    const { logits } = (await classifier(inputs)) as { logits: { data: Float32Array } };
    return Array.from(logits.data);
  },
};

// The page hands each model worker a dedicated MessagePort; the core worker
// holds the other end. RPC runs over that port, never over the worker scope.
scope.onmessage = (event) => {
  const port = (event.data as { port?: MessagePort } | null)?.port;
  if (!port) return;
  scope.onmessage = null;
  serve(port as unknown as Endpoint, handlers);
};
