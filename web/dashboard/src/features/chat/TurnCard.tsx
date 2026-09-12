import { useState } from "react";

import type { Result, Turn } from "../../lib/api-contract";

type Props = {
  turn: Turn;
  onEdit: () => void;
  sequence?: number;
  editing?: boolean;
  editQuery?: string;
  onEditQueryChange?: (query: string) => void;
  onEditSubmit?: () => void;
  onCancelEdit?: () => void;
  pending?: boolean;
};

function scoreText(score: number): string {
  return score.toFixed(3);
}

function CopyButton({ text, label = "Copy" }: { text: string; label?: string }) {
  const [copied, setCopied] = useState(false);

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1400);
    } catch {
      // Clipboard access is optional in insecure browser contexts.
    }
  };

  return (
    <button type="button" onClick={() => void copy()} aria-label={copied ? "Copied" : label} title={label} className="p-1.5 text-[#8b8f81] hover:text-[#20241f] rounded-md transition">
      {copied ? (
        <svg width="15" height="15" viewBox="0 0 24 24" fill="none" aria-hidden="true"><path d="M20 6L9 17l-5-5" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" /></svg>
      ) : (
        <svg width="15" height="15" viewBox="0 0 24 24" fill="none" aria-hidden="true"><rect x="8" y="8" width="14" height="14" rx="2.5" stroke="currentColor" strokeWidth="1.7" /><path d="M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" /></svg>
      )}
    </button>
  );
}

function EditIcon() {
  return <svg width="15" height="15" viewBox="0 0 24 24" fill="none" aria-hidden="true"><path d="M17 3a2.85 2.83 0 1 1 4 4L7.5 20.5 2 22l1.5-5.5Z" stroke="currentColor" strokeWidth="1.7" strokeLinejoin="round" /></svg>;
}

function DocumentIcon() {
  return <svg width="14" height="14" viewBox="0 0 24 24" fill="none" aria-hidden="true"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" stroke="currentColor" strokeWidth="1.8" /><path d="M14 2v6h6M8 13h8M8 17h8" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" /></svg>;
}

function AttachmentCards({ files }: { files: string[] }) {
  if (!files.length) return null;
  return (
    <div className="flex justify-end flex-wrap gap-2 mb-2">
      {files.map((file) => (
        <div key={file} className="w-[118px] bg-[#eeece3] border border-[#e3e2d8] rounded-[12px] px-2.5 py-2.5 flex flex-col items-start gap-2">
          <div className="w-7 h-7 rounded-[7px] bg-[#dce8fb] text-[#2f5db0] flex items-center justify-center"><DocumentIcon /></div>
          <div className="text-[12px] text-[#5c6156] truncate w-full">{file}</div>
        </div>
      ))}
    </div>
  );
}

function EditForm({ editQuery, onEditQueryChange, onSubmit, onCancel }: { editQuery: string; onEditQueryChange: (query: string) => void; onSubmit: () => void; onCancel: () => void }) {
  return (
    <div className="w-full max-w-[82%] mb-3.5">
      <form
        className="rounded-[16px] border border-[#dcd8c9] bg-[#f7f7f4] shadow-[0_20px_40px_-26px_rgba(32,36,31,0.28)] px-[18px] pt-[14px] pb-[12px]"
        onSubmit={(event) => { event.preventDefault(); onSubmit(); }}
      >
        <textarea
          value={editQuery}
          onChange={(event) => onEditQueryChange(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === "Enter" && !event.shiftKey) {
              event.preventDefault();
              onSubmit();
            }
          }}
          rows={2}
          required
          autoFocus
          className="w-full resize-none bg-transparent outline-none text-[16px] leading-[1.6] pb-2.5 max-h-[200px]"
        />
        <div className="flex justify-end gap-2 mt-1">
          <button type="button" onClick={onCancel} className="text-[13.5px] text-[#5c6156] border border-[#dcd8c9] rounded-lg px-3.5 py-1.5 hover:bg-[#eeece3] transition">Cancel</button>
          <button type="submit" className="text-[13.5px] text-white bg-[#2f5d50] rounded-lg px-3.5 py-1.5 hover:bg-[#234840] transition">Send</button>
        </div>
      </form>
    </div>
  );
}

function SearchTrace({ turn }: { turn: Turn }) {
  const [open, setOpen] = useState(!turn.has_answer && !turn.answer);
  const [raw, setRaw] = useState(false);
  const rawSearch = JSON.stringify({
    query: turn.query,
    ...(turn.rewritten_query ? { rewritten: turn.rewritten_query } : {}),
    top_k: turn.top_k,
    generate: turn.generate,
  }, null, 2);

  return (
    <div className="flex gap-2.5 py-1.5">
      <span className="w-4 text-center text-[#8b8f81] flex-none">⊕</span>
      <div className="flex-1 min-w-0">
        <div className="flex flex-wrap gap-1.5 items-baseline text-[14px]">
          <span className="font-semibold text-[#5c6156]">Tool</span><span className="text-[#8b8f81]">·</span>
          <span className="text-[#8b8f81]">Retrieve relevant passages</span>
        </div>
        <div className="ml-0.5 mt-1.5 border-l-[1.5px] border-[#e3e2d8] pl-3.5">
          <button type="button" onClick={() => setOpen((value) => !value)} className="w-full text-left flex items-center gap-1.5 py-0.5 text-[13px] text-[#5c6156] font-mono-ui">
            <svg className={`w-2.5 h-2.5 text-[#8b8f81] transition-transform ${open ? "rotate-90" : ""}`} viewBox="0 0 24 24" fill="none" aria-hidden="true"><path d="m9 6 6 6-6 6" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" /></svg>
            <span><b className="text-[#20241f] font-semibold">Search</b> · &quot;{turn.query}&quot;</span>
          </button>
          {open && (
            <div className="pt-2">
              {turn.results.length ? (
                <div className="bg-[#eeece3] border border-[#e3e2d8] rounded-[7px] py-2">
                  {turn.results.map((result, index) => <SearchResult key={`${result.file_path}-${result.line_start}-${index}`} result={result} />)}
                </div>
              ) : (
                <p className="text-[13px] text-[#8b8f81]">No matching chunks.</p>
              )}
              <button type="button" onClick={() => setRaw((value) => !value)} className="mt-2 font-mono-ui text-[12px] text-[#8b8f81] border border-[#e3e2d8] rounded-md px-2.5 py-1 hover:text-[#5c6156] hover:border-[#dcd8c9]">&lt;/&gt; Inspect</button>
              {raw && <pre className="mt-2 font-mono-ui text-[12.5px] leading-relaxed text-[#5c6156] bg-[#eeece3] border border-[#e3e2d8] rounded-[7px] px-3 py-2 overflow-x-auto">{rawSearch}{`\n→ ${turn.count} chunks · ${turn.elapsed_ms}ms${turn.from_cache ? " · cached" : ""}`}</pre>}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

function SearchResult({ result }: { result: Result }) {
  return (
    <div className="grid grid-cols-[14px_1fr_auto] gap-2.5 items-baseline px-3 py-[3px] font-mono-ui text-[12.5px]">
      <span className="text-[#8b8f81]">·</span>
      <span className="text-[#5c6156] truncate"><b className="text-[#20241f] font-medium">{result.file_path}</b>{result.header ? ` · ${result.header}` : ""} · L{result.line_start}<span className="sr-only">{result.text}</span></span>
      <span className="text-[#8b8f81]">{scoreText(result.score)}</span>
    </div>
  );
}

function ThinkTrace({ turn }: { turn: Turn }) {
  const [contextOpen, setContextOpen] = useState(false);
  return (
    <div className="flex gap-2.5 py-1.5">
      <span className="w-4 text-center text-[#8b8f81] flex-none">⊗</span>
      <div className="flex-1 min-w-0">
        <div className="text-[14px]">
          <span className="font-semibold text-[#5c6156]">Think</span><span className="text-[#8b8f81]"> · </span>
          <span className="text-[#8b8f81]">Reordering retrieved chunks (lost-in-middle) and generating the answer.</span>
        </div>
        {turn.rewritten_query && (
          <div className="ml-0.5 mt-1.5 border-l-[1.5px] border-[#e3e2d8] pl-3.5 font-mono-ui text-[12.5px] leading-relaxed">
            <span className="text-[#8b8f81]">Rewrite · </span>
            <span className="text-[#8b8f81]">&quot;{turn.query}&quot; </span>
            <span className="text-[#8b8f81]">→ </span>
            <b className="text-[#20241f] font-medium">&quot;{turn.rewritten_query}&quot;</b>
          </div>
        )}
        {turn.generate_error && <div className="ml-0.5 mt-1.5 text-[14px] text-[#b04a3f]">{turn.generate_error}</div>}
        {turn.prompt && (
          <div className="ml-0.5 mt-1.5 border-l-[1.5px] border-[#e3e2d8] pl-3.5">
            <button type="button" onClick={() => setContextOpen((value) => !value)} className="font-mono-ui text-[12px] text-[#8b8f81] border border-[#e3e2d8] rounded-md px-2.5 py-1 hover:text-[#5c6156] hover:border-[#dcd8c9]">&lt;/&gt; Inspect context sent to the LLM</button>
            {contextOpen && <pre className="mt-2 font-mono-ui text-[12.5px] leading-relaxed text-[#5c6156] bg-[#eeece3] border border-[#e3e2d8] rounded-[7px] px-3 py-2 overflow-x-auto whitespace-pre-wrap">{turn.prompt}</pre>}
          </div>
        )}
      </div>
    </div>
  );
}

function PendingTrace() {
  return (
    <div className="flex gap-2.5 py-1.5">
      <span className="w-4 text-center text-[#8b8f81] flex-none animate-pulse">⊕</span>
      <div className="flex-1 min-w-0 text-[14px]">
        <span className="font-semibold text-[#5c6156]">Tool</span><span className="text-[#8b8f81]"> · </span>
        <span className="text-[#8b8f81] animate-pulse">Retrieving relevant passages…</span>
      </div>
    </div>
  );
}

export default function TurnCard({ turn, sequence = turn.sequence ?? 0, editing = false, editQuery = turn.query, onEdit, onEditQueryChange = () => {}, onEditSubmit = () => {}, onCancelEdit = () => {}, pending = false }: Props) {
  if (pending) {
    return (
      <div className="mb-8" data-pending-turn>
        <AttachmentCards files={turn.attached_files ?? []} />
        <div className="flex flex-col items-end mb-3.5">
          <div data-copy-text className="text-[16px] leading-[1.5] bg-[#2f5d5017] border border-[#2f5d5017] rounded-tl-[14px] rounded-tr-[14px] rounded-bl-[14px] rounded-br-[3px] px-[15px] py-[10px] max-w-[82%] whitespace-pre-wrap">{turn.query}</div>
        </div>
        <PendingTrace />
      </div>
    );
  }

  const hasAnswer = Boolean(turn.has_answer || turn.answer);
  const hasTrace = !turn.error && (hasAnswer || turn.stream_url || turn.generate_error);
  const showThinkTrace = hasTrace && (!turn.streaming || !hasAnswer || Boolean(turn.generate_error));

  return (
    <div className="mb-8" data-turn-sequence={sequence}>
      <AttachmentCards files={turn.attached_files ?? []} />
      <div className="sr-only" aria-label="Retrieved passage text">
        {turn.results.map((result, index) => <span key={`${result.file_path}-${result.line_start}-text-${index}`}>{result.text}</span>)}
      </div>

      <div className="nadir-turn-q flex flex-col items-end">
        {!editing ? (
          <div data-copy-text className="text-[16px] leading-[1.5] bg-[#2f5d5017] border border-[#2f5d5017] rounded-tl-[14px] rounded-tr-[14px] rounded-bl-[14px] rounded-br-[3px] px-[15px] py-[10px] max-w-[82%] whitespace-pre-wrap">{turn.query}</div>
        ) : (
          <EditForm
            editQuery={editQuery}
            onEditQueryChange={onEditQueryChange}
            onSubmit={onEditSubmit}
            onCancel={onCancelEdit}
          />
        )}

        {!editing && (
          <div className="nadir-msg-actions flex items-center gap-0.5 mt-1.5 mb-3.5">
            <CopyButton text={turn.query} label="Copy question" />
            <button type="button" onClick={onEdit} aria-label="Edit question" title="Edit" className="p-1.5 text-[#8b8f81] hover:text-[#20241f] rounded-md transition"><EditIcon /></button>
          </div>
        )}
      </div>

      {turn.error ? (
        <p className="text-[14px] text-[#b04a3f]">{turn.error}</p>
      ) : (
        <>
          <div className="flex flex-col gap-0.5 mb-3.5">
            <SearchTrace turn={turn} />
            {showThinkTrace && <ThinkTrace turn={turn} />}
          </div>

          {hasAnswer && (
            <div className={`nadir-turn-a text-[17px] text-[#20241f] leading-[1.7] ${turn.streaming ? "nadir-streaming" : ""}`}>
              <p className="whitespace-pre-wrap" data-copy-text>{turn.answer}</p>
              <div className="nadir-msg-actions flex items-center gap-0.5 mt-1.5 -ml-1.5"><CopyButton text={turn.answer ?? ""} /></div>
            </div>
          )}
          {!hasAnswer && turn.streaming && (
            <div className="nadir-turn-a text-[17px] text-[#20241f] leading-[1.7] nadir-streaming">
              <p className="nadir-answer" data-copy-text />
              <div className="nadir-msg-actions flex items-center gap-0.5 mt-1.5 -ml-1.5"><CopyButton text="" /></div>
            </div>
          )}
        </>
      )}
    </div>
  );
}
