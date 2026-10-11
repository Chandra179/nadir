import { useEffect, useState, type ReactNode } from "react";
import { setTransport } from "../lib/transport";
import { startBrowserRuntime, type BrowserRuntime } from "./client";
import type { ResolvedRuntime } from "./config";
import type { Progress } from "./types";

// StrictMode runs effects twice in development; a module-level promise keeps
// a single runtime (and a single model download) per page.
let starting: Promise<BrowserRuntime> | undefined;
let progressListener: (p: Progress) => void = () => {};

function start(resolved: ResolvedRuntime): Promise<BrowserRuntime> {
  starting ??= startBrowserRuntime(resolved.config, (p) => progressListener(p));
  return starting;
}

type Props = { resolved: ResolvedRuntime; children: ReactNode };

/** Starts the in-browser runtime, shows model-download progress, then renders
 *  the app over the in-page transport. */
export default function RuntimeGate({ resolved, children }: Props) {
  const [progress, setProgress] = useState<Record<string, Progress>>({});
  const [ready, setReady] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    progressListener = (p) => setProgress((current) => ({ ...current, [p.stage]: p }));
    let cancelled = false;
    start(resolved)
      .then(async (runtime) => {
        if (cancelled) return;
        setTransport(runtime.transport);
        // Ask the browser not to evict the corpus under storage pressure.
        await navigator.storage?.persist?.().catch(() => false);
        setReady(true);
      })
      .catch((e: unknown) => {
        starting = undefined;
        if (!cancelled) setError(e instanceof Error ? e.message : String(e));
      });
    return () => {
      cancelled = true;
    };
  }, [resolved]);

  if (ready) {
    return (
      <>
        {resolved.notices.length > 0 && (
          <div role="status" className="bg-amber-50 px-4 py-2 text-xs text-amber-900">
            {resolved.notices.join(" ")}
          </div>
        )}
        {children}
      </>
    );
  }

  return (
    <main className="mx-auto flex h-full max-w-md flex-col justify-center gap-4 px-6">
      <h1 className="font-serif-display text-2xl font-semibold">Starting Nadir in your browser</h1>
      <p className="text-sm text-neutral-600">
        Your documents and questions stay on this device. Models download once and are cached.
      </p>
      {error ? (
        <div role="alert" className="feedback feedback-err">
          <p>Could not start: {error}</p>
          <button className="mt-2 underline" onClick={() => location.reload()}>
            Retry
          </button>
        </div>
      ) : (
        <ul className="flex flex-col gap-3" aria-live="polite">
          {Object.values(progress).map((p) => (
            <li key={p.stage} className="text-xs">
              <div className="mb-1 flex justify-between">
                <span>{p.label}</span>
                <span>{p.fraction === undefined ? "…" : `${Math.round(p.fraction * 100)}%`}</span>
              </div>
              <progress className="h-1 w-full" max={1} value={p.fraction ?? undefined} aria-label={p.label} />
            </li>
          ))}
        </ul>
      )}
    </main>
  );
}
