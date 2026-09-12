import type { MouseEvent } from "react";

type Props = {
  onClose: () => void;
  onDeleteAll: () => void;
};

export default function AdministrationPanel({ onClose, onDeleteAll }: Props) {
  const closeOnBackdrop = (event: MouseEvent<HTMLDivElement>) => {
    if (event.target === event.currentTarget) onClose();
  };

  return (
    <div
      className="fixed inset-0 z-10 flex items-center justify-center bg-black/70 px-5"
      onMouseDown={closeOnBackdrop}
    >
      <section className="w-full max-w-md rounded-2xl border border-slate-700 bg-slate-900 p-6 shadow-2xl">
        <div className="flex items-center justify-between">
          <h2 className="text-lg font-medium text-white">Settings</h2>
          <button onClick={onClose} className="text-slate-500 hover:text-white" aria-label="Close settings">
            ×
          </button>
        </div>
        <p className="mt-2 text-sm leading-6 text-slate-500">
          Destructive operations are explicit and affect only the selected data type.
        </p>
        <button
          onClick={onDeleteAll}
          className="mt-6 w-full rounded-lg border border-rose-500/30 px-4 py-3 text-left text-sm text-rose-300 hover:bg-rose-500/10"
        >
          Delete all conversations
          <span className="mt-1 block text-xs text-rose-400/60">Indexed documents are kept.</span>
        </button>
        <button
          onClick={onClose}
          className="mt-4 w-full rounded-lg bg-slate-800 px-4 py-3 text-sm text-slate-300 hover:bg-slate-700"
        >
          Close
        </button>
      </section>
    </div>
  );
}
