import { CLOSED, FetchEventSource } from "../runtime/eventsource";

/** The dashboard talks to the API through a transport so the same UI can run
 *  against a server (browser fetch/EventSource) or the in-page WASM runtime. */
export type EventSourceLike = {
  readyState: number;
  onerror: ((event: Event) => void) | null;
  addEventListener(type: string, listener: (event: MessageEvent<string>) => void): void;
  close(): void;
};

export type Transport = {
  fetch: typeof fetch;
  openEventSource(url: string): EventSourceLike;
};

export const EVENT_SOURCE_CLOSED = CLOSED;

const serverTransport: Transport = {
  fetch: (input, init) => globalThis.fetch(input, init),
  openEventSource: (url) => new EventSource(url) as unknown as EventSourceLike,
};

let current: Transport = serverTransport;

export function getTransport(): Transport {
  return current;
}

export function setTransport(transport: Transport): void {
  current = transport;
}

export function resetTransport(): void {
  current = serverTransport;
}

/** Builds a transport whose API calls and event streams use the given fetch. */
export function transportFromFetch(doFetch: typeof fetch): Transport {
  return {
    fetch: doFetch,
    openEventSource: (url) => new FetchEventSource(url, doFetch),
  };
}
