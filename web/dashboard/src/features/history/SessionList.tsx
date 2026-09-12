import type { Session } from "../../lib/api-contract";

type Props = {
  sessions: Session[];
  activeSessionId: string | null;
  onSelect: (id: string) => void;
  onNew: () => void;
  onDelete: (session: Session) => void;
  onSettings: () => void;
  onClose: () => void;
};

function SettingsIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <path d="M12 8.5a3.5 3.5 0 1 0 0 7 3.5 3.5 0 0 0 0-7Z" stroke="currentColor" strokeWidth="1.8" />
      <path d="m19.4 15 .1.1a2 2 0 1 1-2.8 2.8l-.1-.1a2 2 0 0 0-3.4 1.4v.2a2 2 0 1 1-4 0v-.2a2 2 0 0 0-3.4-1.4l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1A2 2 0 0 0 1.6 12a2 2 0 1 1 0-4h.2a2 2 0 0 0 1.4-3.4l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1A2 2 0 0 0 9.4.4h.2a2 2 0 1 1 4 0v.2A2 2 0 0 0 17 2l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1A2 2 0 0 0 21.2 8h.2a2 2 0 1 1 0 4h-.2a2 2 0 0 0-1.8 3Z" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
    </svg>
  );
}

function MoreIcon() {
  return (
    <svg width="14" height="14" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
      <circle cx="12" cy="5" r="1.7" />
      <circle cx="12" cy="12" r="1.7" />
      <circle cx="12" cy="19" r="1.7" />
    </svg>
  );
}

export default function SessionList({ sessions, activeSessionId, onSelect, onNew, onDelete, onSettings, onClose }: Props) {
  return (
    <aside className="h-full bg-[#f7f7f4] border-r border-[#e3e2d8] flex flex-col min-h-0">
      <div className="flex items-baseline gap-1.5 px-[18px] pt-[18px] pb-3.5">
        <span className="font-serif-display font-semibold text-[18px]">Nadir</span>
        <span className="text-[12px] text-[#8b8f81]">reading room</span>
      </div>

      <button
        type="button"
        onClick={() => { onNew(); onClose(); }}
        className="mx-3 mb-3.5 rounded-[9px] border border-[#dcd8c9] bg-white text-[#5c6156] text-[14px] px-3 py-2 flex items-center gap-2 hover:border-[#2f5d50] hover:text-[#234840] transition"
      >
        + New chat
      </button>

      <div className="px-[18px] pb-1.5 text-[11px] tracking-[0.1em] uppercase text-[#8b8f81]">Chats</div>
      <div className="px-2 flex flex-col gap-0.5 flex-1 overflow-y-auto min-h-0">
        {sessions.length === 0 ? (
          <div className="px-2.5 py-2 text-[13px] text-[#8b8f81]">No chats yet</div>
        ) : (
          sessions.map((session) => (
            <div
              key={session.id}
              className={`group flex items-center rounded-[7px] ${activeSessionId === session.id ? "bg-[#eeece3]" : "hover:bg-[#eeece3]"}`}
            >
              <button
                type="button"
                onClick={() => { onSelect(session.id); onClose(); }}
                className="flex-1 min-w-0 text-left text-[#20241f] text-[14px] px-2.5 py-2 truncate"
              >
                {session.title || "New chat"}
              </button>
              <button
                type="button"
                aria-label="Chat options"
                title="Chat options"
                onClick={() => onDelete(session)}
                className="flex-none w-6 h-6 mr-1 rounded-[6px] flex items-center justify-center text-[#8b8f81] hover:bg-[#e3e0d3] hover:text-[#20241f] transition"
              >
                <MoreIcon />
              </button>
            </div>
          ))
        )}
      </div>

      <div className="mt-auto px-[18px] py-3.5 border-t border-[#e3e2d8] flex items-center gap-2">
        <div className="w-6 h-6 rounded-full bg-[#e4e1d5] border border-[#dcd8c9] flex-none" />
        <div className="min-w-0 flex-1">
          <div className="text-[13px] text-[#5c6156]">Local</div>
          <div className="text-[11px] text-[#8b8f81] font-mono-ui truncate">nomic-embed-text</div>
        </div>
        <button
          type="button"
          onClick={onSettings}
          aria-label="Open settings"
          title="Settings"
          className="w-8 h-8 rounded-lg flex items-center justify-center text-[#8b8f81] hover:bg-[#eeece3] hover:text-[#20241f] transition"
        >
          <SettingsIcon />
        </button>
      </div>
    </aside>
  );
}
