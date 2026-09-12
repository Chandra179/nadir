import type { MouseEvent, ReactNode } from "react";

type Props = {
  deleteAllOpen: boolean;
  deleteAllBusy: boolean;
  deleteAllError: string;
  onClose: () => void;
  onOpenDeleteAll: () => void;
  onCloseDeleteAll: () => void;
  onDeleteAll: () => void;
};

function TrashIcon({ small = false }: { small?: boolean }) {
  return (
    <svg width={small ? 14 : 18} height={small ? 14 : 18} viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <path d="M4 7h16M10 4h4M6 7l1 13a1 1 0 0 0 1 .93h8A1 1 0 0 0 17 20l1-13M10 11v6M14 11v6" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

function Backdrop({ children, className, onMouseDown }: { children: ReactNode; className: string; onMouseDown: (event: MouseEvent<HTMLDivElement>) => void }) {
  return <div className={className} onMouseDown={onMouseDown}>{children}</div>;
}

export default function AdministrationPanel({ deleteAllOpen, deleteAllBusy, deleteAllError, onClose, onOpenDeleteAll, onCloseDeleteAll, onDeleteAll }: Props) {
  const closeSettingsOnBackdrop = (event: MouseEvent<HTMLDivElement>) => {
    if (event.target === event.currentTarget) onClose();
  };
  const closeDeleteAllOnBackdrop = (event: MouseEvent<HTMLDivElement>) => {
    if (event.target === event.currentTarget && !deleteAllBusy) onCloseDeleteAll();
  };

  return (
    <>
      {!deleteAllOpen && (
        <Backdrop className="fixed inset-0 z-40 flex items-end md:items-center justify-center bg-black/30 p-4" onMouseDown={closeSettingsOnBackdrop}>
          <section className="bg-[#f7f7f4] border border-[#dcd8c9] rounded-[16px] shadow-xl w-full max-w-[380px] p-5">
            <div className="flex items-start justify-between gap-4 mb-5">
              <div>
                <div className="font-serif-display font-semibold text-[18px]">Settings</div>
                <p className="text-[13px] text-[#8b8f81] mt-1">Manage this local reading room.</p>
              </div>
              <button type="button" onClick={onClose} aria-label="Close settings" className="w-7 h-7 rounded-lg text-[#8b8f81] hover:bg-[#eeece3] hover:text-[#20241f]">×</button>
            </div>
            <div className="border-t border-[#e3e2d8] pt-4">
              <div className="text-[11px] tracking-[0.1em] uppercase text-[#8b8f81] mb-2">Chat history</div>
              <button type="button" onClick={onOpenDeleteAll} className="w-full flex items-center gap-3 rounded-[10px] border border-[#e8c9c4] bg-[#fff9f8] px-3.5 py-3 text-left text-[#b04a3f] hover:bg-[#fcefeb] transition">
                <TrashIcon />
                <span>
                  <span className="block text-[13.5px] font-medium">Delete all chats</span>
                  <span className="block text-[12px] text-[#a56a63] mt-0.5">Permanently remove every saved conversation</span>
                </span>
              </button>
            </div>
          </section>
        </Backdrop>
      )}

      {deleteAllOpen && (
        <Backdrop className="fixed inset-0 z-50 flex items-center justify-center bg-black/30 p-4" onMouseDown={closeDeleteAllOnBackdrop}>
          <section className="bg-[#f7f7f4] border border-[#dcd8c9] rounded-[16px] shadow-xl w-full max-w-[360px] p-6">
            <div className="w-10 h-10 rounded-full bg-[#f6e4e0] text-[#b04a3f] flex items-center justify-center mb-4"><TrashIcon /></div>
            <div className="font-serif-display font-semibold text-[18px] mb-1">Delete all chats?</div>
            <p className="text-[13px] leading-[1.5] text-[#5c6156] mb-2">This permanently removes every saved conversation and cannot be undone.</p>
            <p className="text-[12px] text-[#8b8f81] mb-5">Your indexed documents will not be affected.</p>
            {deleteAllError && <p className="text-[12px] text-[#b04a3f] mb-3">{deleteAllError}</p>}
            <div className="flex justify-end gap-2">
              <button type="button" onClick={onCloseDeleteAll} disabled={deleteAllBusy} className="text-[13.5px] text-[#5c6156] border border-[#dcd8c9] rounded-lg px-3.5 py-2 hover:bg-[#eeece3] transition disabled:opacity-50">Cancel</button>
              <button type="button" onClick={onDeleteAll} disabled={deleteAllBusy} className="text-[13.5px] text-white bg-[#b04a3f] rounded-lg px-3.5 py-2 flex items-center gap-2 hover:bg-[#a03e34] transition disabled:opacity-60">
                {deleteAllBusy ? "Deleting…" : "Delete all"}
              </button>
            </div>
          </section>
        </Backdrop>
      )}
    </>
  );
}
