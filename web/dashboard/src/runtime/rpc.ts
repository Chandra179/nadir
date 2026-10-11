/** Minimal request/response RPC over a MessagePort or Worker, with per-call
 *  progress events and cancellation. */

export type Endpoint = {
  postMessage(message: unknown, transfer?: Transferable[]): void;
  onmessage: ((event: MessageEvent) => void) | null;
};

type Request = { t: "req"; id: number; method: string; args: unknown[] };
type Response = { t: "res"; id: number; ok: boolean; value?: unknown; error?: string };
type Event = { t: "evt"; id: number; name: string; value: unknown };
type Abort = { t: "abort"; id: number };
type Message = Request | Response | Event | Abort;

export type CallOptions = {
  transfer?: Transferable[];
  signal?: AbortSignal;
  onEvent?: (name: string, value: unknown) => void;
};

export class RpcClient {
  private nextID = 1;
  private pending = new Map<number, { resolve(v: unknown): void; reject(e: Error): void; onEvent?: CallOptions["onEvent"] }>();

  constructor(private endpoint: Endpoint) {
    endpoint.onmessage = (event) => {
      const message = event.data as Message;
      if (message.t === "res") {
        const call = this.pending.get(message.id);
        if (!call) return;
        this.pending.delete(message.id);
        if (message.ok) call.resolve(message.value);
        else call.reject(new Error(message.error ?? "rpc failed"));
      } else if (message.t === "evt") {
        this.pending.get(message.id)?.onEvent?.(message.name, message.value);
      }
    };
  }

  call<T = unknown>(method: string, args: unknown[] = [], options: CallOptions = {}): Promise<T> {
    const { signal } = options;
    if (signal?.aborted) return Promise.reject(new DOMException("aborted", "AbortError"));
    const id = this.nextID++;
    return new Promise<T>((resolve, reject) => {
      const onAbort = () => {
        this.endpoint.postMessage({ t: "abort", id } satisfies Abort);
        this.pending.delete(id);
        reject(new DOMException("aborted", "AbortError"));
      };
      signal?.addEventListener("abort", onAbort, { once: true });
      const done = () => signal?.removeEventListener("abort", onAbort);
      this.pending.set(id, {
        resolve: (v) => (done(), resolve(v as T)),
        reject: (e) => (done(), reject(e)),
        onEvent: options.onEvent,
      });
      this.endpoint.postMessage({ t: "req", id, method, args } satisfies Request, options.transfer ?? []);
    });
  }
}

export type CallContext = {
  signal: AbortSignal;
  emit(name: string, value?: unknown, transfer?: Transferable[]): void;
};

export type Handlers = Record<string, (ctx: CallContext, ...args: never[]) => Promise<unknown> | unknown>;

/** Serves handlers on an endpoint. A handler's return value is sent as the
 *  response; ctx.emit streams intermediate events to the caller. */
export function serve(endpoint: Endpoint, handlers: Handlers): void {
  const inflight = new Map<number, AbortController>();
  endpoint.onmessage = async (event) => {
    const message = event.data as Message;
    if (message.t === "abort") {
      inflight.get(message.id)?.abort();
      return;
    }
    if (message.t !== "req") return;
    const { id, method, args } = message;
    const controller = new AbortController();
    inflight.set(id, controller);
    const ctx: CallContext = {
      signal: controller.signal,
      emit: (name, value, transfer) => endpoint.postMessage({ t: "evt", id, name, value } satisfies Event, transfer ?? []),
    };
    try {
      const handler = handlers[method];
      if (!handler) throw new Error(`unknown method ${method}`);
      const value = await (handler as (ctx: CallContext, ...a: unknown[]) => unknown)(ctx, ...args);
      endpoint.postMessage({ t: "res", id, ok: true, value } satisfies Response);
    } catch (error) {
      endpoint.postMessage({ t: "res", id, ok: false, error: error instanceof Error ? error.message : String(error) } satisfies Response);
    } finally {
      inflight.delete(id);
    }
  };
}
