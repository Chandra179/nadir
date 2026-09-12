import { useCallback, useEffect, useRef, useState } from "react";
import { streamURL } from "../lib/http";
import type { Turn } from "../lib/api-contract";

type Options = {
  onToken: (turnID: string, text: string) => void;
  onDone: (turnID: string) => void;
  onGenerationError: (turnID: string, text: string) => void;
  onResync: () => void;
  onDisconnected: () => void;
};

/** Owns browser subscription lifecycle for one domain Chat turn. */
export function useTurnStream(options: Options) {
  const sourceRef = useRef<EventSource | null>(null);
  const optionsRef = useRef(options);
  const lastEventIDRef = useRef(0);
  const manuallyClosedRef = useRef(false);
  const [activeTurnID, setActiveTurnID] = useState<string | null>(null);

  useEffect(() => {
    optionsRef.current = options;
  }, [options]);

  const close = useCallback(() => {
    manuallyClosedRef.current = true;
    sourceRef.current?.close();
    sourceRef.current = null;
    setActiveTurnID(null);
  }, []);

  useEffect(() => close, [close]);

  const start = useCallback((turn: Turn) => {
    if (!turn.stream_url || !turn.turn_id) return;
    close();
    manuallyClosedRef.current = false;
    lastEventIDRef.current = 0;
    const turnID = turn.turn_id;
    const source = new EventSource(streamURL(turn.stream_url));
    sourceRef.current = source;
    setActiveTurnID(turnID);
    const isNewEvent = (event: Event): boolean => {
      const eventID = Number((event as MessageEvent<string>).lastEventId);
      if (!Number.isFinite(eventID) || eventID <= 0) return true;
      if (eventID <= lastEventIDRef.current) return false;
      lastEventIDRef.current = eventID;
      return true;
    };
    const finish = () => {
      manuallyClosedRef.current = true;
      source.close();
      if (sourceRef.current === source) sourceRef.current = null;
      setActiveTurnID(null);
    };
    source.addEventListener("token", (event) => {
      if (isNewEvent(event)) optionsRef.current.onToken(turnID, (event as MessageEvent<string>).data);
    });
    source.addEventListener("done", () => {
      finish();
      optionsRef.current.onDone(turnID);
    });
    source.addEventListener("generror", (event) => {
      if (!isNewEvent(event)) return;
      finish();
      optionsRef.current.onGenerationError(turnID, (event as MessageEvent<string>).data);
    });
    source.addEventListener("resync", (event) => {
      if (!isNewEvent(event)) return;
      finish();
      optionsRef.current.onResync();
    });
    source.onerror = () => {
      if (sourceRef.current !== source || manuallyClosedRef.current) return;
      if (source.readyState === EventSource.CLOSED) {
        sourceRef.current = null;
        setActiveTurnID(null);
        optionsRef.current.onDisconnected();
      }
      // CONNECTING is the native EventSource retry state. Keeping the same
      // source open makes the browser send Last-Event-ID on reconnect.
    };
  }, [close]);

  return { activeTurnID, start, close };
}
