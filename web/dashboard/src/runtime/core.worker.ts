/** Hosts the Go WASM core. The page talks to it only through RPC `fetch`, which
 *  serves the same versioned JSON/SSE API as the server runtime. */
import { MockModels } from "./mock-models";
import { pdfToMarkdown } from "./pdf";
import { RpcClient, serve, type Endpoint } from "./rpc";
import { MemorySnapshotStore, OpfsSnapshotStore, type SnapshotStore } from "./storage";
import type { Models, Progress, RuntimeConfig } from "./types";
import { WebModels } from "./web-models";

type Scope = Endpoint & { postMessage(m: unknown, t?: Transferable[]): void };
const scope = self as unknown as Scope;

type GoHandle = {
  fetch(request: unknown): Promise<void>;
  snapshot(): Promise<{ documents: Uint8Array; history: Uint8Array }>;
  close(): Promise<void>;
};
type NadirGlobal = { start(options: unknown): Promise<GoHandle> };

let runtime: GoHandle | undefined;
let store: SnapshotStore = new MemorySnapshotStore();
let dirty = false;
let persistTimer: ReturnType<typeof setTimeout> | undefined;
let persisting: Promise<void> = Promise.resolve();

async function loadGo(config: RuntimeConfig): Promise<void> {
  const execSource = await (await fetch(config.wasmExecURL)).text();
  // wasm_exec.js defines globalThis.Go; indirect eval runs it in global scope.
  (0, eval)(execSource);
  const go = new (globalThis as unknown as { Go: new () => { importObject: WebAssembly.Imports; run(i: WebAssembly.Instance): Promise<void> } }).Go();
  const bytes = await (await fetch(config.wasmURL)).arrayBuffer();
  const { instance } = await WebAssembly.instantiate(bytes, go.importObject);
  void go.run(instance); // resolves only when the Go program exits
  for (let i = 0; i < 100 && !(globalThis as { nadir?: unknown }).nadir; i++) await new Promise((r) => setTimeout(r, 10));
  if (!(globalThis as { nadir?: unknown }).nadir) throw new Error("WASM runtime did not register");
}

function schedulePersist(): void {
  dirty = true;
  clearTimeout(persistTimer);
  persistTimer = setTimeout(() => void flush(), 400);
}

function flush(): Promise<void> {
  persisting = persisting.then(async () => {
    if (!dirty || !runtime) return;
    dirty = false;
    try {
      await store.save(await runtime.snapshot());
    } catch (error) {
      dirty = true;
      console.error("nadir: persisting snapshot failed", error);
    }
  });
  return persisting;
}

function createModels(config: RuntimeConfig, ml?: Endpoint, llm?: Endpoint): Models {
  if (config.models.kind === "mock") return new MockModels();
  if (!ml) throw new Error("web models need an ml worker port");
  return new WebModels(config.models, new RpcClient(ml), llm ? new RpcClient(llm) : undefined);
}

serve(scope, {
  async init(ctx, config: RuntimeConfig, mlPort?: MessagePort, llmPort?: MessagePort) {
    const emit = (p: Progress) => ctx.emit("progress", p);
    emit({ stage: "runtime", label: "Loading WASM core" });
    await loadGo(config);
    const models = createModels(config, mlPort as unknown as Endpoint, llmPort as unknown as Endpoint);
    await models.warmup(emit);

    store = config.persist ? new OpfsSnapshotStore(config.storageName) : new MemorySnapshotStore();
    const snapshot = await store.load();
    const nadir = (globalThis as unknown as { nadir: NadirGlobal }).nadir;
    runtime = await nadir.start({
      dimensions: models.dimensions,
      model: models.embeddingModel,
      embed: async (text: string) => (await models.embed([text]))[0],
      embedBatch: (texts: string[]) => models.embed(texts),
      generatorModel: models.generatorModel,
      generate: models.generate && ((prompt: string, ctl: { signal: AbortSignal; emit(t: string): void }) => models.generate!(prompt, ctl.signal, ctl.emit)),
      complete: models.complete && ((system: string, user: string) => models.complete!(system, user)),
      rerank: models.rerank && ((query: string, passages: string[]) => models.rerank!(query, passages)),
      convert: async (name: string, data: Uint8Array) => pdfToMarkdown(name, data),
      contextWindow: config.models.kind === "web" ? config.models.contextWindow : undefined,
      maxOutputTokens: config.models.kind === "web" ? config.models.maxOutputTokens : undefined,
      snapshot: snapshot.documents || snapshot.history ? snapshot : undefined,
    });
    return { embeddingModel: models.embeddingModel, generatorModel: models.generatorModel ?? null, restored: Boolean(snapshot.documents) };
  },

  async fetch(ctx, request: { method: string; url: string; headers: Record<string, string>; body?: Uint8Array }) {
    if (!runtime) throw new Error("runtime not initialised");
    await runtime.fetch({
      ...request,
      signal: ctx.signal,
      onHead: (status: number, headers: Record<string, string>) => ctx.emit("head", { status, headers }),
      onChunk: (chunk: Uint8Array) => ctx.emit("chunk", chunk, [chunk.buffer]),
    });
    // Mutations and finished streams (which persist the answer) change state.
    if (request.method !== "GET" || request.headers["Accept"] === "text/event-stream") schedulePersist();
    return true;
  },

  async persist() {
    dirty = true;
    await flush();
    return true;
  },

  async close() {
    await flush();
    await runtime?.close();
    runtime = undefined;
    return true;
  },
});
