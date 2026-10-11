/** Answer generation and query rewriting with WebLLM (WebGPU). Requests are
 *  serialized: one engine serves one generation at a time. */
import { CreateMLCEngine, type MLCEngine } from "@mlc-ai/web-llm";
import { serve, type Endpoint, type Handlers } from "./rpc";

type Scope = { onmessage: ((event: MessageEvent) => void) | null; postMessage(m: unknown, t?: Transferable[]): void };
const scope = self as unknown as Scope;

let engine: MLCEngine | undefined;
let queue: Promise<unknown> = Promise.resolve();

function serialized<T>(task: () => Promise<T>): Promise<T> {
  const run = queue.then(task, task);
  queue = run.catch(() => undefined);
  return run;
}

const handlers: Handlers = {
  async load(ctx, config: { model: string; contextWindow?: number }) {
    if (!("gpu" in navigator)) throw new Error("WebGPU is not available in this browser");
    engine = await CreateMLCEngine(
      config.model,
      {
        initProgressCallback: (report) => ctx.emit("progress", { stage: "llm", label: config.model, fraction: report.progress }),
      },
      config.contextWindow ? { context_window_size: config.contextWindow } : undefined,
    );
    return true;
  },

  generate(ctx, prompt: string, options: { maxTokens?: number }) {
    return serialized(async () => {
      if (!engine) throw new Error("LLM not loaded");
      const stream = await engine.chat.completions.create({
        messages: [{ role: "user", content: prompt }],
        stream: true,
        temperature: 0,
        max_tokens: options.maxTokens,
      });
      const stop = () => engine?.interruptGenerate();
      ctx.signal.addEventListener("abort", stop, { once: true });
      try {
        for await (const chunk of stream) {
          const token = chunk.choices[0]?.delta?.content;
          if (token) ctx.emit("token", token);
        }
      } finally {
        ctx.signal.removeEventListener("abort", stop);
      }
      return true;
    });
  },

  complete(_ctx, system: string, user: string) {
    return serialized(async () => {
      if (!engine) throw new Error("LLM not loaded");
      const reply = await engine.chat.completions.create({
        messages: [
          { role: "system", content: system },
          { role: "user", content: user },
        ],
        temperature: 0,
        max_tokens: 128,
      });
      return reply.choices[0]?.message?.content ?? "";
    });
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
