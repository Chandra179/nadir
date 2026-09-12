import type { KeyboardEvent } from "react";

type Props = {
  query: string;
  attachedFiles: string[];
  editSequence: number | null;
  busy: boolean;
  activeTurnID: string | null;
  onQueryChange: (query: string) => void;
  onSubmit: () => void;
  onCancelEdit: () => void;
  onStop: () => void;
};

export default function Composer({
  query,
  attachedFiles,
  editSequence,
  busy,
  activeTurnID,
  onQueryChange,
  onSubmit,
  onCancelEdit,
  onStop,
}: Props) {
  const handleKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key === "Enter" && !event.shiftKey) {
      event.preventDefault();
      onSubmit();
    }
  };

  return (
    <div className="border-t border-slate-800 bg-nadir-ink/95 px-5 py-5 md:px-10">
      <div className="mx-auto max-w-4xl">
        {attachedFiles.length > 0 && (
          <div className="mb-3 flex flex-wrap gap-2">
            {attachedFiles.map((file) => (
              <span key={file} className="rounded-full bg-slate-800 px-3 py-1 text-xs text-slate-400">
                {file}
              </span>
            ))}
          </div>
        )}

        {editSequence !== null && (
          <div className="mb-3 flex items-center justify-between rounded-lg border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-xs text-amber-200">
            <span>Editing turn {editSequence + 1}; later turns will be replaced.</span>
            <button onClick={onCancelEdit} className="text-amber-400 hover:text-amber-200">
              Cancel
            </button>
          </div>
        )}

        <div className="flex items-end gap-3 rounded-2xl border border-slate-700 bg-slate-900 p-3 shadow-2xl shadow-black/20 focus-within:border-cyan-500/50">
          <textarea
            value={query}
            onChange={(event) => onQueryChange(event.target.value)}
            onKeyDown={handleKeyDown}
            placeholder="Ask a question…"
            rows={1}
            className="max-h-40 min-h-10 flex-1 resize-y bg-transparent px-2 py-2 text-sm text-slate-100 outline-none placeholder:text-slate-600"
          />
          {activeTurnID ? (
            <button
              onClick={onStop}
              className="rounded-xl bg-rose-500/15 px-4 py-2.5 text-sm text-rose-300 hover:bg-rose-500/25"
            >
              Stop
            </button>
          ) : (
            <button
              onClick={onSubmit}
              disabled={busy || !query.trim()}
              className="rounded-xl bg-cyan-400 px-4 py-2.5 text-sm font-medium text-slate-950 transition hover:bg-cyan-300 disabled:cursor-not-allowed disabled:opacity-40"
            >
              {busy ? "Working…" : "Send"}
            </button>
          )}
        </div>
        <p className="mt-2 text-center text-[11px] text-slate-700">Enter to send · Shift+Enter for a new line</p>
      </div>
    </div>
  );
}
