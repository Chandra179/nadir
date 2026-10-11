import { describe, expect, it } from "vitest";
import { RpcClient, serve } from "./rpc";

function pair() {
  const { port1, port2 } = new MessageChannel();
  return { a: port1 as never, b: port2 as never, close: () => (port1.close(), port2.close()) };
}

describe("rpc", () => {
  it("returns results, streams events and propagates errors", async () => {
    const { a, b, close } = pair();
    serve(b, {
      add: (_ctx, x: number, y: number) => x + y,
      count: async (ctx, n: number) => {
        for (let i = 1; i <= n; i++) ctx.emit("tick", i);
        return "done";
      },
      boom: () => {
        throw new Error("nope");
      },
    });
    const client = new RpcClient(a);
    expect(await client.call("add", [2, 3])).toBe(5);
    const ticks: unknown[] = [];
    expect(await client.call("count", [3], { onEvent: (_n, v) => ticks.push(v) })).toBe("done");
    expect(ticks).toEqual([1, 2, 3]);
    await expect(client.call("boom")).rejects.toThrow("nope");
    await expect(client.call("missing")).rejects.toThrow("unknown method");
    close();
  });

  it("aborts a call on both sides", async () => {
    const { a, b, close } = pair();
    let serverAborted = false;
    serve(b, {
      wait: (ctx) =>
        new Promise((resolve) => {
          ctx.signal.addEventListener("abort", () => {
            serverAborted = true;
            resolve("aborted");
          });
        }),
    });
    const client = new RpcClient(a);
    const controller = new AbortController();
    const call = client.call("wait", [], { signal: controller.signal });
    controller.abort();
    await expect(call).rejects.toThrow("aborted");
    for (let i = 0; i < 100 && !serverAborted; i++) await new Promise((r) => setTimeout(r, 5));
    expect(serverAborted).toBe(true);
    close();
  });
});
