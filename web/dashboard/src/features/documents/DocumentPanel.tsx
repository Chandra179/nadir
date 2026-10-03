import { useCallback, useEffect, useState } from "react";
import type { DocumentsResponse } from "../../lib/api-contract";
import { listDocuments } from "./api";

export default function DocumentPanel({ revision }: { revision: number }) {
  const [open, setOpen] = useState(false);
  const [status, setStatus] = useState<DocumentsResponse | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const refresh = useCallback(async () => {
    setLoading(true);
    setError(null);
    try { setStatus(await listDocuments()); }
    catch (cause) { setError(cause instanceof Error ? cause.message : "Could not load indexed files"); }
    finally { setLoading(false); }
  }, []);
  useEffect(() => { if (open) void refresh(); }, [open, revision, refresh]);

  return (
    <div className="ml-auto relative">
      <button type="button" aria-expanded={open} aria-controls="document-inventory" onClick={() => setOpen((value) => !value)} className="text-[13px] text-[#5c6156] border border-[#dcd8c9] rounded-lg px-3 py-1.5 hover:bg-[#eeece3]">Knowledge base</button>
      {open && (
        <section id="document-inventory" aria-label="Indexed documents" className="absolute right-0 top-full mt-2 z-20 w-[340px] max-w-[85vw] max-h-[60vh] overflow-auto bg-[#f7f7f4] border border-[#dcd8c9] rounded-xl shadow-lg p-4">
          <div className="flex items-center justify-between gap-2">
            <h2 className="font-semibold text-sm">{status ? `${status.count} indexed files` : "Indexed files"}</h2>
            <button type="button" onClick={() => void refresh()} disabled={loading} className="text-xs text-[#2f5d50]">{loading ? "Loading…" : "Refresh"}</button>
          </div>
          <p className="text-xs text-[#8b8f81] mt-2">Upload Markdown using the paperclip below. PDF import requires Docling to be enabled.</p>
          {error && <p role="alert" className="text-xs text-[#b04a3f] mt-2">{error}</p>}
          {status && <ul className="mt-3 space-y-1 text-xs break-all">{status.documents.map((file) => <li key={file.file_path}>{file.file_path}</li>)}</ul>}
          {status?.count === 0 && <p className="text-xs mt-3">No documents indexed yet.</p>}
          {status?.last_import && (
            <div className="border-t border-[#dcd8c9] mt-3 pt-3 text-xs">
              <p>Last import this server run: {new Date(status.last_import.completed_at).toLocaleString()}</p>
              <p className="mt-1">{status.last_import.result.processed} processed, {status.last_import.result.skipped} unchanged, {status.last_import.result.failed} failed.</p>
              {status.last_import.result.error && <p className="text-[#b04a3f] mt-1">{status.last_import.result.error}</p>}
              {status.last_import.result.files?.filter((file) => file.status === "failed").map((file, index) => <p key={`${file.name}-${index}`} className="text-[#b04a3f] mt-1">{file.name}: {file.error}{file.published ? " (update is visible; cleanup needs a retry)" : ""}</p>)}
            </div>
          )}
        </section>
      )}
    </div>
  );
}
