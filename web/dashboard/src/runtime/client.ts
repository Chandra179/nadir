/** Main-thread entry for the in-browser runtime: starts the workers and exposes
 *  a fetch/EventSource transport that serves the API entirely in the page. */
import { RpcClient, type Endpoint } from "./rpc";
import { transportFromFetch, type Transport } from "../lib/transport";
import type { Progress, RuntimeConfig } from "./types";

export type BrowserRuntime = {
  transport: Transport;
  fetch: typeof fetch;
  info: { embeddingModel: string; generatorModel: string | null; restored: boolean };
  /** Flushes the stored snapshot now. */
  persist(): Promise<void>;
  close(): Promise<void>;
};

const NULL_BODY_STATUS = new Set([101, 204, 205, 304]);

export async function startBrowserRuntime(config: RuntimeConfig, onProgress: (p: Progress) => void = () => {}): Promise<BrowserRuntime> {
  const core = new Worker(new URL("./core.worker.ts", import.meta.url), { type: "module" });
  const workers: Worker[] = [core];
  const ports: MessagePort[] = [];
  // Each model worker serves RPC on a dedicated MessagePort: the core worker
  // owns port2 and the model worker receives port1.
  let mlPort: MessagePort | undefined;
  let llmPort: MessagePort | undefined;
  if (config.models.kind === "web") {
    const ml = new Worker(new URL("./ml.worker.ts", import.meta.url), { type: "module" });
    workers.push(ml);
    const mlChannel = new MessageChannel();
    ml.postMessage({ port: mlChannel.port1 }, [mlChannel.port1]);
    mlPort = mlChannel.port2;
    ports.push(mlPort);
    if (config.models.llmModel) {
      const llm = new Worker(new URL("./llm.worker.ts", import.meta.url), { type: "module" });
      workers.push(llm);
      const llmChannel = new MessageChannel();
      llm.postMessage({ port: llmChannel.port1 }, [llmChannel.port1]);
      llmPort = llmChannel.port2;
      ports.push(llmPort);
    }
  }

  const rpc = new RpcClient(core as unknown as Endpoint);
  const transfer = ports.slice();
  const info = await rpc.call<BrowserRuntime["info"]>("init", [config, mlPort, llmPort], {
    transfer,
    onEvent: (name, value) => name === "progress" && onProgress(value as Progress),
  });

  const doFetch: typeof fetch = async (input, init) => {
    const request = new Request(input as RequestInfo, init);
    const url = new URL(request.url, location.href);
    const body = request.body ? new Uint8Array(await request.arrayBuffer()) : undefined;
    const headers: Record<string, string> = {};
    request.headers.forEach((value, key) => (headers[key.replace(/(^|-)(\w)/g, (_m, dash: string, c: string) => dash + c.toUpperCase())] = value));

    return new Promise<Response>((resolve, reject) => {
      let controller!: ReadableStreamDefaultController<Uint8Array>;
      let headSeen = false;
      const stream = new ReadableStream<Uint8Array>({ start: (c) => void (controller = c), cancel: () => abort.abort() });
      const abort = new AbortController();
      request.signal.addEventListener("abort", () => abort.abort(), { once: true });
      rpc
        .call("fetch", [{ method: request.method, url: url.pathname + url.search, headers, body }], {
          transfer: body ? [body.buffer] : [],
          signal: abort.signal,
          onEvent: (name, value) => {
            if (name === "head") {
              const { status, headers: responseHeaders } = value as { status: number; headers: Record<string, string> };
              headSeen = true;
              resolve(new Response(NULL_BODY_STATUS.has(status) ? null : stream, { status, headers: responseHeaders }));
            } else if (name === "chunk") {
              controller.enqueue(value as Uint8Array);
            }
          },
        })
        .then(() => controller.close())
        .catch((error: Error) => {
          if (headSeen) {
            try {
              controller.error(error);
            } catch {
              /* stream already closed by the consumer */
            }
          } else reject(error);
        });
    });
  };

  return {
    transport: transportFromFetch(doFetch),
    fetch: doFetch,
    info,
    persist: async () => void (await rpc.call("persist")),
    close: async () => {
      await rpc.call("close");
      for (const worker of workers) worker.terminate();
    },
  };
}
