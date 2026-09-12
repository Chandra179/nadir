import { useState } from "react";

import type { Turn } from "../../lib/api-contract";

function scorePercent(score: number, max: number): number {
  if (max <= 0) return 100;
  return Math.max(4, Math.round((score / max) * 100));
}

type Props = {
  turn: Turn;
  onEdit: () => void;
};

export default function TurnCard({ turn, onEdit }: Props) {
  const [copied, setCopied] = useState(false);
  const maxScore = Math.max(...turn.results.map((result) => result.score), 0);

  const copyAnswer = async () => {
    if (!turn.answer) return;
    await navigator.clipboard.writeText(turn.answer);
    setCopied(true);
    window.setTimeout(() => setCopied(false), 1500);
  };

  return (
    <article className="rounded-2xl border border-slate-800 bg-slate-900/45 p-5 md:p-6">
      <div className="flex items-start justify-between gap-5">
        <div className="min-w-0 flex-1">
          <p className="whitespace-pre-wrap text-base leading-7 text-white">{turn.query}</p>
          {turn.attached_files?.length ? (
            <div className="mt-3 flex flex-wrap gap-2">
              {turn.attached_files.map((file) => (
                <span key={file} className="rounded-full bg-slate-800 px-2.5 py-1 text-[11px] text-slate-500">
                  {file}
                </span>
              ))}
            </div>
          ) : null}
        </div>
        <button
          onClick={onEdit}
          className="shrink-0 rounded-lg border border-transparent px-2 py-1 text-xs text-slate-600 hover:border-slate-700 hover:text-cyan-300"
        >
          Edit
        </button>
      </div>

      {turn.rewritten_query && (
        <p className="mt-4 border-l-2 border-violet-400/40 pl-3 text-xs text-slate-500">
          Searched as: {turn.rewritten_query}
        </p>
      )}
      {turn.error && <p className="mt-4 text-sm text-rose-300">{turn.error}</p>}

      {turn.results.length > 0 && (
        <div className="mt-6 space-y-3">
          <p className="text-[11px] font-semibold uppercase tracking-[0.18em] text-slate-600">
            Retrieved context · {turn.count}
          </p>
          {turn.results.map((result, index) => (
            <div
              key={`${result.file_path}-${result.line_start}-${index}`}
              className="rounded-xl border border-slate-800 bg-slate-950/50 p-3"
            >
              <div className="flex items-center justify-between gap-3">
                <p className="truncate text-xs text-cyan-300">
                  {result.file_path}{result.header ? ` · ${result.header}` : ""}
                </p>
                <span className="shrink-0 text-[11px] text-slate-600">{result.score.toFixed(3)}</span>
              </div>
              <div className="mt-2 h-1 overflow-hidden rounded-full bg-slate-800">
                <div
                  className="h-full rounded-full bg-cyan-500/60"
                  style={{ width: `${scorePercent(result.score, maxScore)}%` }}
                />
              </div>
              <p className="mt-3 whitespace-pre-wrap text-sm leading-6 text-slate-400">{result.text}</p>
              <p className="mt-2 text-[11px] text-slate-700">Line {result.line_start}</p>
            </div>
          ))}
        </div>
      )}

      {turn.answer && (
        <div className="mt-6 border-t border-slate-800 pt-5">
          <div className="flex items-start justify-between gap-3">
            <p className="whitespace-pre-wrap text-sm leading-7 text-slate-300">{turn.answer}</p>
            <button
              onClick={() => void copyAnswer()}
              className="shrink-0 rounded-md px-2 py-1 text-[11px] text-slate-600 hover:text-cyan-300"
            >
              {copied ? "Copied" : "Copy"}
            </button>
          </div>
        </div>
      )}
      {turn.generate_error && <p className="mt-4 text-sm text-amber-300">Generation: {turn.generate_error}</p>}
      {turn.streaming && !turn.answer && !turn.generate_error && (
        <p className="mt-5 animate-pulse text-xs text-slate-600">Generating…</p>
      )}
      <div className="mt-5 flex gap-3 text-[11px] text-slate-700">
        <span>{turn.elapsed_ms} ms</span>
        {turn.from_cache && <span>cached retrieval</span>}
      </div>
    </article>
  );
}
