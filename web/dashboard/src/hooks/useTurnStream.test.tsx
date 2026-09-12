import { useEffect } from "react";
import { render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useTurnStream } from "./useTurnStream";
import type { Turn } from "../lib/api-contract";

class FakeEventSource extends EventTarget {
  static readonly CONNECTING = 0;
  static readonly OPEN = 1;
  static readonly CLOSED = 2;
  static instances: FakeEventSource[] = [];

  readonly url: string;
  readyState = FakeEventSource.OPEN;
  onerror: ((event: Event) => void) | null = null;

  constructor(url: string) {
    super();
    this.url = url;
    FakeEventSource.instances.push(this);
  }

  close() {
    this.readyState = FakeEventSource.CLOSED;
  }
}

const turn: Turn = {
  query: "question",
  session_id: "session-1",
  top_k: 5,
  generate: true,
  results: [],
  count: 0,
  elapsed_ms: 0,
  from_cache: false,
  turn_id: "turn-1",
  stream_url: "/api/v1/turns/turn-1/events",
  streaming: true,
};

function Probe({ onToken, onDone, onDisconnected }: {
  onToken: (id: string, text: string) => void;
  onDone: (id: string) => void;
  onDisconnected: () => void;
}) {
  const { activeTurnID, start } = useTurnStream({
    onToken,
    onDone,
    onGenerationError: vi.fn(),
    onResync: vi.fn(),
    onDisconnected,
  });
  useEffect(() => start(turn), [start]);
  return <output>{activeTurnID ?? "idle"}</output>;
}

describe("useTurnStream", () => {
  beforeEach(() => {
    FakeEventSource.instances = [];
    vi.stubGlobal("EventSource", FakeEventSource);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("ignores duplicate cursors and completes the active turn", async () => {
    const onToken = vi.fn();
    const onDone = vi.fn();
    render(<Probe onToken={onToken} onDone={onDone} onDisconnected={vi.fn()} />);
    const source = FakeEventSource.instances[0];

    source.dispatchEvent(new MessageEvent("token", { data: "one", lastEventId: "1" }));
    source.dispatchEvent(new MessageEvent("token", { data: "duplicate", lastEventId: "1" }));
    expect(onToken).toHaveBeenCalledTimes(1);
    expect(onToken).toHaveBeenCalledWith("turn-1", "one");

    source.dispatchEvent(new MessageEvent("done", { data: "1", lastEventId: "2" }));
    expect(onDone).toHaveBeenCalledWith("turn-1");
    await waitFor(() => expect(screen.getByText("idle")).toBeInTheDocument());
  });

  it("keeps the native EventSource open while it retries", () => {
    const onDisconnected = vi.fn();
    render(<Probe onToken={vi.fn()} onDone={vi.fn()} onDisconnected={onDisconnected} />);
    const source = FakeEventSource.instances[0];

    source.readyState = FakeEventSource.CONNECTING;
    source.onerror?.(new Event("error"));
    expect(onDisconnected).not.toHaveBeenCalled();
    expect(screen.getByText("turn-1")).toBeInTheDocument();

    source.readyState = FakeEventSource.CLOSED;
    source.onerror?.(new Event("error"));
    expect(onDisconnected).toHaveBeenCalledTimes(1);
  });
});
