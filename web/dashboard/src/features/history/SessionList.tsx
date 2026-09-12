import type { Session } from "../../lib/api-contract";

type Props = {
  sessions: Session[];
  activeSessionId: string | null;
  onSelect: (id: string) => void;
  onNew: () => void;
  onDelete: (session: Session) => void;
};

export default function SessionList({ sessions, activeSessionId, onSelect, onNew, onDelete }: Props) {
  return (
    <aside className="flex w-full shrink-0 flex-col border-b border-slate-800 bg-slate-950/70 md:h-screen md:w-72 md:border-b-0 md:border-r">
      <div className="flex items-center justify-between px-5 py-5">
        <div>
          <p className="text-lg font-semibold tracking-tight text-white">Nadir</p>
          <p className="text-xs text-slate-500">Grounded knowledge workspace</p>
        </div>
        <button aria-label="Start a new chat" onClick={onNew} className="rounded-lg border border-slate-700 px-3 py-2 text-sm text-slate-300 transition hover:border-cyan-500 hover:text-cyan-300">
          New
        </button>
      </div>
      <div className="scrollbar-thin flex-1 overflow-y-auto px-3 pb-4">
        <p className="px-2 py-2 text-[11px] font-semibold uppercase tracking-[0.18em] text-slate-600">Conversations</p>
        {sessions.length === 0 ? (
          <p className="px-2 py-4 text-sm text-slate-600">No saved chats yet.</p>
        ) : (
          <div className="space-y-1">
            {sessions.map((session) => (
              <div key={session.id} className={`group flex items-center gap-2 rounded-xl border px-2 py-1 transition ${activeSessionId === session.id ? "border-cyan-500/40 bg-cyan-500/10" : "border-transparent hover:border-slate-800 hover:bg-slate-900"}`}>
                <button onClick={() => onSelect(session.id)} className="min-w-0 flex-1 px-2 py-2 text-left">
                  <p className="truncate text-sm text-slate-200">{session.title || "New chat"}</p>
                  <p className="mt-1 text-xs text-slate-600">{session.turn_count} {session.turn_count === 1 ? "turn" : "turns"}</p>
                </button>
                <button aria-label={`Delete ${session.title}`} onClick={() => onDelete(session)} className="rounded-md px-2 py-2 text-slate-700 opacity-0 transition hover:text-rose-300 group-hover:opacity-100">×</button>
              </div>
            ))}
          </div>
        )}
      </div>
    </aside>
  );
}
