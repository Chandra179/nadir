import { useEffect, useRef, useState, type KeyboardEvent } from "react";

type Props = {
  query: string;
  attachedFiles: string[];
  busy: boolean;
  activeTurnID: string | null;
  uploading: boolean;
  uploadMessage: string | null;
  onQueryChange: (query: string) => void;
  onSubmit: () => void;
  onStop: () => void;
  onUpload: (files: File[]) => Promise<void>;
  onRemoveAttachment: (name: string) => void;
};

export default function Composer({
  query,
  attachedFiles,
  busy,
  activeTurnID,
  uploading,
  uploadMessage,
  onQueryChange,
  onSubmit,
  onStop,
  onUpload,
  onRemoveAttachment,
}: Props) {
  const [fileMenuOpen, setFileMenuOpen] = useState(false);
  const menuRef = useRef<HTMLDivElement | null>(null);
  const inputRef = useRef<HTMLTextAreaElement | null>(null);

  useEffect(() => {
    const closeMenu = (event: MouseEvent) => {
      if (!menuRef.current?.contains(event.target as Node)) setFileMenuOpen(false);
    };
    document.addEventListener("mousedown", closeMenu);
    return () => document.removeEventListener("mousedown", closeMenu);
  }, []);

  useEffect(() => {
    const input = inputRef.current;
    if (!input) return;
    input.style.height = "auto";
    input.style.height = `${Math.min(input.scrollHeight, 160)}px`;
  }, [query]);

  const handleKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key === "Enter" && !event.shiftKey) {
      event.preventDefault();
      onSubmit();
    }
  };

  const stopMode = busy || activeTurnID !== null;

  return (
    <div className="flex-none border-[#e3e2d8] px-4 md:px-7 pt-3.5 pb-5">
      <form
        className="mx-auto max-w-[768px]"
        onSubmit={(event) => { event.preventDefault(); onSubmit(); }}
      >
        {attachedFiles.length > 0 && (
          <div id="composer-attachments" className="flex flex-wrap gap-2.5 mb-2.5">
            {attachedFiles.map((file) => (
              <div key={file} className="relative flex items-center gap-2.5 w-[196px] flex-none bg-[#eeece3] border border-[#e3e2d8] rounded-[14px] pl-2.5 pr-7 py-2">
                <div className="w-8 h-8 rounded-[9px] bg-[#dce8fb] text-[#2f5db0] flex items-center justify-center flex-none">
                  <DocumentIcon />
                </div>
                <div className="min-w-0">
                  <div className="text-[12px] font-semibold text-[#20241f] truncate">{file}</div>
                  <div className="text-[10.5px] text-[#8b8f81]">File</div>
                </div>
                <button
                  type="button"
                  onClick={() => onRemoveAttachment(file)}
                  aria-label={`Dismiss ${file}`}
                  className="absolute top-1 right-1 w-4 h-4 rounded-full bg-[#20241f] text-white flex items-center justify-center text-[9px] leading-none hover:bg-black"
                >
                  ×
                </button>
              </div>
            ))}
          </div>
        )}

        <div className="rounded-[16px] border border-[#dcd8c9] bg-[#f7f7f4] shadow-[0_20px_40px_-26px_rgba(32,36,31,0.28)] px-[18px] pt-[16px] pb-[12px]">
          <textarea
            ref={inputRef}
            value={query}
            onChange={(event) => onQueryChange(event.target.value)}
            onKeyDown={handleKeyDown}
            placeholder="Ask about your documents…"
            rows={1}
            required
            className="w-full resize-none bg-transparent outline-none text-[16px] leading-[1.6] pb-2.5 placeholder-[#8b8f81] max-h-[160px]"
          />
          <div className="flex items-center gap-2">
            <div ref={menuRef} className="relative">
              <button
                type="button"
                onClick={() => setFileMenuOpen((open) => !open)}
                aria-expanded={fileMenuOpen}
                aria-haspopup="menu"
                aria-label="Add files"
                className="w-[29px] h-[29px] rounded-full border border-[#dcd8c9] text-[#5c6156] text-[17px] leading-none flex items-center justify-center hover:border-[#2f5d50] hover:text-[#234840] transition"
              >
                +
              </button>
              {fileMenuOpen && (
                <div className="absolute bottom-[calc(100%+8px)] left-0 min-w-[200px] bg-white border border-[#dcd8c9] rounded-[11px] shadow-lg p-1.5 z-20" role="menu">
                  <label className="flex items-center gap-2 px-2.5 py-2 rounded-[7px] text-[14px] text-[#5c6156] cursor-pointer hover:bg-[#eeece3] hover:text-[#20241f]">
                    <input
                      type="file"
                      accept=".md,.markdown"
                      multiple
                      className="hidden"
                      disabled={busy || uploading}
                      onChange={(event) => {
                        const files = Array.from(event.target.files ?? []);
                        if (files.length) void onUpload(files);
                        event.currentTarget.value = "";
                        setFileMenuOpen(false);
                      }}
                    />
                    <span className="w-4 text-center text-[#8b8f81]">▤</span> Import Markdown
                  </label>
                  <div className="flex items-center gap-2 px-2.5 py-2 rounded-[7px] text-[14px] text-[#8b8f81]">
                    <span className="w-4 text-center text-[#8b8f81]">▥</span> Import PDF
                    <span className="ml-auto text-[10px] tracking-wide uppercase border border-[#e3e2d8] rounded-full px-1.5 py-0.5">Soon</span>
                  </div>
                  <div className="px-2.5 pt-1.5 pb-0.5 text-[12px] text-[#8b8f81]">More formats later</div>
                </div>
              )}
            </div>
            <button
              type={stopMode ? "button" : "submit"}
              onClick={stopMode ? onStop : undefined}
              aria-label={stopMode ? "Stop response" : "Send message"}
              className="ml-auto w-[29px] h-[29px] rounded-full bg-[#2f5d50] text-white flex items-center justify-center hover:bg-[#234840] transition"
            >
              {stopMode ? <StopIcon /> : <SendIcon />}
            </button>
          </div>
        </div>
        {uploadMessage && <div className="feedback feedback-err mt-2 text-center">{uploadMessage}</div>}
        <div id="composer-hint" className="text-center text-[12px] text-[#8b8f81] mt-2">Every answer is grounded in retrieved passages, shown above it.</div>
      </form>
    </div>
  );
}

function DocumentIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" stroke="currentColor" strokeWidth="1.8" />
      <path d="M14 2v6h6M8 13h8M8 17h8" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" />
    </svg>
  );
}

function SendIcon() {
  return (
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <path d="M12 19V5M6 11l6-6 6 6" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

function StopIcon() {
  return (
    <svg width="14" height="14" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
      <rect x="7" y="7" width="10" height="10" rx="1.5" />
    </svg>
  );
}
