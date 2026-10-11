/** A minimal EventSource built on a fetch function, so server-sent events can
 *  be served by the in-page runtime. It covers what the dashboard relies on:
 *  named events with lastEventId, Last-Event-ID resume on reconnect, 204 as a
 *  terminal "stop reconnecting" signal, and close(). */

export const CONNECTING = 0;
export const OPEN = 1;
export const CLOSED = 2;

type Listener = (event: MessageEvent<string>) => void;

export class FetchEventSource {
  static readonly CONNECTING = CONNECTING;
  static readonly OPEN = OPEN;
  static readonly CLOSED = CLOSED;

  readyState: number = CONNECTING;
  onerror: ((event: Event) => void) | null = null;
  onopen: ((event: Event) => void) | null = null;
  private listeners = new Map<string, Set<Listener>>();
  private controller = new AbortController();
  private lastEventId = "";
  private retryMs: number;

  constructor(
    readonly url: string,
    private doFetch: typeof fetch,
    options: { retryMs?: number } = {},
  ) {
    this.retryMs = options.retryMs ?? 1000;
    void this.run();
  }

  addEventListener(type: string, listener: Listener): void {
    let set = this.listeners.get(type);
    if (!set) this.listeners.set(type, (set = new Set()));
    set.add(listener);
  }

  removeEventListener(type: string, listener: Listener): void {
    this.listeners.get(type)?.delete(listener);
  }

  close(): void {
    this.readyState = CLOSED;
    this.controller.abort();
  }

  private dispatch(type: string, data: string, id: string): void {
    const event = new MessageEvent<string>(type, { data, lastEventId: id });
    for (const listener of this.listeners.get(type) ?? []) listener(event);
  }

  private fail(): void {
    this.onerror?.(new Event("error"));
  }

  private async run(): Promise<void> {
    while (this.readyState !== CLOSED) {
      this.readyState = CONNECTING;
      try {
        const headers: Record<string, string> = { Accept: "text/event-stream" };
        if (this.lastEventId) headers["Last-Event-ID"] = this.lastEventId;
        const response = await this.doFetch(this.url, { headers, signal: this.controller.signal });
        // 204 means the stream is finished and the client must not reconnect.
        if (response.status === 204 || !response.ok || !response.body) {
          this.readyState = CLOSED;
          this.fail();
          return;
        }
        this.readyState = OPEN;
        this.onopen?.(new Event("open"));
        await this.consume(response.body);
      } catch {
        if (this.readyState === CLOSED) return;
      }
      if (this.readyState === CLOSED) return;
      // Connection dropped: report it and retry, resuming from lastEventId.
      this.readyState = CONNECTING;
      this.fail();
      await new Promise((resolve) => setTimeout(resolve, this.retryMs));
    }
  }

  private async consume(body: ReadableStream<Uint8Array>): Promise<void> {
    const reader = body.getReader();
    const decoder = new TextDecoder();
    let buffer = "";
    for (;;) {
      const { value, done } = await reader.read();
      if (done) return;
      buffer += decoder.decode(value, { stream: true }).replace(/\r\n?/g, "\n");
      for (let end = buffer.indexOf("\n\n"); end !== -1; end = buffer.indexOf("\n\n")) {
        this.parseBlock(buffer.slice(0, end));
        buffer = buffer.slice(end + 2);
      }
    }
  }

  private parseBlock(block: string): void {
    let event = "message";
    let id = this.lastEventId;
    const data: string[] = [];
    for (const line of block.split("\n")) {
      if (line.startsWith(":")) continue;
      const colon = line.indexOf(":");
      const field = colon === -1 ? line : line.slice(0, colon);
      const value = colon === -1 ? "" : line.slice(colon + 1).replace(/^ /, "");
      if (field === "event") event = value;
      else if (field === "data") data.push(value);
      else if (field === "id") id = value;
    }
    if (data.length === 0) return;
    this.lastEventId = id;
    this.dispatch(event, data.join("\n"), id);
  }
}
