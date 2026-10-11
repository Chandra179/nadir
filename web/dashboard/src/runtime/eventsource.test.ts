import { describe, expect, it } from "vitest";
import { CLOSED, FetchEventSource } from "./eventsource";

function sseResponse(chunks: string[], status = 200): Response {
  const encoder = new TextEncoder();
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      for (const chunk of chunks) controller.enqueue(encoder.encode(chunk));
      controller.close();
    },
  });
  return new Response(status === 204 ? null : body, { status });
}

const until = async (condition: () => boolean) => {
  for (let i = 0; i < 200 && !condition(); i++) await new Promise((r) => setTimeout(r, 5));
  expect(condition()).toBe(true);
};

describe("FetchEventSource", () => {
  it("dispatches named events with ids, even when split across chunks", async () => {
    const doFetch = (async () =>
      sseResponse(["id: 1\nevent: token\nda", "ta: hel\ndata: lo\n\nid: 2\nevent: done\ndata: 1\n\n"])) as unknown as typeof fetch;
    const source = new FetchEventSource("/stream", doFetch, { retryMs: 5 });
    const seen: Array<[string, string, string]> = [];
    for (const type of ["token", "done"]) {
      source.addEventListener(type, (e) => seen.push([type, e.data, e.lastEventId]));
    }
    await until(() => seen.length === 2);
    source.close();
    expect(seen).toEqual([
      ["token", "hel\nlo", "1"],
      ["done", "1", "2"],
    ]);
  });

  it("resumes with Last-Event-ID after the connection drops", async () => {
    const requests: Array<Record<string, string>> = [];
    let call = 0;
    const doFetch = (async (_url: string, init?: RequestInit) => {
      requests.push({ ...(init?.headers as Record<string, string>) });
      call++;
      return call === 1 ? sseResponse(["id: 7\nevent: token\ndata: a\n\n"]) : sseResponse(["id: 8\nevent: token\ndata: b\n\n"]);
    }) as unknown as typeof fetch;
    const source = new FetchEventSource("/stream", doFetch, { retryMs: 5 });
    const tokens: string[] = [];
    source.addEventListener("token", (e) => tokens.push(e.data));
    await until(() => tokens.length === 2);
    source.close();
    expect(requests[1]["Last-Event-ID"]).toBe("7");
    expect(tokens).toEqual(["a", "b"]);
  });

  it("stops without reconnecting on 204 and reports an error", async () => {
    let calls = 0;
    const doFetch = (async () => (calls++, sseResponse([], 204))) as unknown as typeof fetch;
    const source = new FetchEventSource("/stream", doFetch, { retryMs: 5 });
    let errors = 0;
    source.onerror = () => errors++;
    await until(() => source.readyState === CLOSED);
    await new Promise((r) => setTimeout(r, 30));
    expect(calls).toBe(1);
    expect(errors).toBe(1);
  });

  it("close() aborts the request and ends the loop", async () => {
    let aborted = false;
    const doFetch = ((_url: string, init?: RequestInit) =>
      new Promise((_resolve, reject) => {
        init?.signal?.addEventListener("abort", () => {
          aborted = true;
          reject(new DOMException("aborted", "AbortError"));
        });
      })) as unknown as typeof fetch;
    const source = new FetchEventSource("/stream", doFetch);
    source.close();
    await until(() => aborted);
    expect(source.readyState).toBe(CLOSED);
  });
});
