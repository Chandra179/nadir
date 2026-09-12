import Notice from "../../components/Notice";

type Props = {
  onUpload: (files: File[]) => Promise<void>;
  onReset: () => Promise<void>;
  uploading: boolean;
  message: string | null;
};

export default function DocumentPanel({ onUpload, onReset, uploading, message }: Props) {
  return (
    <section className="rounded-2xl border border-slate-800 bg-slate-900/60 p-4">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h2 className="text-sm font-medium text-slate-200">Knowledge base</h2>
          <p className="mt-1 text-xs leading-5 text-slate-500">Add Markdown or PDF files to the indexed corpus.</p>
        </div>
        <label className="cursor-pointer rounded-lg border border-cyan-500/40 px-3 py-2 text-xs text-cyan-300 transition hover:bg-cyan-500/10">
          {uploading ? "Uploading…" : "Upload"}
          <input type="file" multiple accept=".md,.markdown,.pdf" className="hidden" disabled={uploading} onChange={(event) => {
            const files = Array.from(event.target.files ?? []);
            if (files.length) void onUpload(files);
            event.currentTarget.value = "";
          }} />
        </label>
      </div>
      {message && <div className="mt-3"><Notice>{message}</Notice></div>}
      <button onClick={() => { if (window.confirm("Reset every indexed document?")) void onReset(); }} className="mt-4 text-xs text-rose-400 transition hover:text-rose-300">Reset document index</button>
    </section>
  );
}
