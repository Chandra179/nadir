import { useCallback, useEffect, useRef, useState } from "react";

import type { Session, Turn } from "../../lib/api-contract";
import { useTurnStream } from "../../hooks/useTurnStream";
import AdministrationPanel from "../administration/AdministrationPanel";
import { cancelTurn, startTurn } from "../chat/api";
import Composer from "../chat/Composer";
import TurnCard from "../chat/TurnCard";
import { ingestDocuments } from "../documents/api";
import { deleteAllSessions, deleteSession, getSession, listSessions } from "../history/api";
import SessionList from "../history/SessionList";

function withSequence(turn: Turn, sequence: number): Turn {
  return { ...turn, sequence };
}

function messageFrom(cause: unknown, fallback: string): string {
  return cause instanceof Error ? cause.message : fallback;
}

export default function WorkspacePage() {
  const [sessions, setSessions] = useState<Session[]>([]);
  const [activeSessionID, setActiveSessionID] = useState<string | null>(null);
  const [turns, setTurns] = useState<Turn[]>([]);
  const [pendingTurn, setPendingTurn] = useState<Turn | null>(null);
  const [query, setQuery] = useState("");
  const [editQuery, setEditQuery] = useState("");
  const [editSequence, setEditSequence] = useState<number | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [sidebarOpen, setSidebarOpen] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [deleteSessionTarget, setDeleteSessionTarget] = useState<Session | null>(null);
  const [deleteAllOpen, setDeleteAllOpen] = useState(false);
  const [deleteAllBusy, setDeleteAllBusy] = useState(false);
  const [deleteAllError, setDeleteAllError] = useState("");
  const [uploading, setUploading] = useState(false);
  const [documentMessage, setDocumentMessage] = useState<string | null>(null);
  const [attachedFiles, setAttachedFiles] = useState<string[]>([]);
  const readerRef = useRef<HTMLDivElement | null>(null);

  const refreshSessions = useCallback(async () => {
    try {
      setSessions((await listSessions()).sessions);
    } catch (cause) {
      setError(messageFrom(cause, "Could not load conversations"));
    }
  }, []);

  const onToken = useCallback((turnID: string, text: string) => {
    setTurns((current) => current.map((turn) => (
      turn.turn_id === turnID
        ? { ...turn, answer: `${turn.answer ?? ""}${text}`, has_answer: true }
        : turn
    )));
  }, []);

  const onDone = useCallback((turnID: string) => {
    setTurns((current) => current.map((turn) => (
      turn.turn_id === turnID ? { ...turn, streaming: false } : turn
    )));
    setBusy(false);
    void refreshSessions();
  }, [refreshSessions]);

  const onGenerationError = useCallback((turnID: string, text: string) => {
    setBusy(false);
    setTurns((current) => current.map((turn) => (
      turn.turn_id === turnID
        ? { ...turn, generate_error: text, streaming: false }
        : turn
    )));
  }, []);

  const onResync = useCallback(() => {
    setBusy(false);
    if (!activeSessionID) {
      setError("The live answer window expired. Start the conversation again.");
      return;
    }
    void getSession(activeSessionID)
      .then((detail) => {
        setTurns(detail.turns.map(withSequence));
        setError("The live answer window expired; the conversation was reloaded.");
      })
      .catch((cause) => setError(messageFrom(cause, "Could not reload conversation")));
  }, [activeSessionID]);

  const onDisconnected = useCallback(() => {
    setBusy(false);
    setError("The answer stream disconnected.");
  }, []);

  const { activeTurnID, start: startStream, close: closeStream } = useTurnStream({
    onToken,
    onDone,
    onGenerationError,
    onResync,
    onDisconnected,
  });

  useEffect(() => {
    const reader = readerRef.current;
    if (reader) reader.scrollTop = reader.scrollHeight;
  }, [pendingTurn, turns]);

  const selectSession = useCallback(async (id: string) => {
    closeStream();
    setPendingTurn(null);
    setError(null);
    try {
      const detail = await getSession(id);
      setActiveSessionID(id);
      setTurns(detail.turns.map(withSequence));
      setEditSequence(null);
      setEditQuery("");
      window.history.pushState({}, "", `/sessions/${encodeURIComponent(id)}`);
    } catch (cause) {
      setError(messageFrom(cause, "Could not load conversation"));
    }
  }, [closeStream]);

  const newChat = useCallback(() => {
    closeStream();
    setActiveSessionID(null);
    setTurns([]);
    setPendingTurn(null);
    setQuery("");
    setEditQuery("");
    setEditSequence(null);
    setAttachedFiles([]);
    setError(null);
    setSidebarOpen(false);
    window.history.pushState({}, "", "/");
  }, [closeStream]);

  useEffect(() => {
    void refreshSessions();
    const id = window.location.pathname.match(/^\/sessions\/([^/]+)$/)?.[1];
    if (id) void selectSession(decodeURIComponent(id));

    const onPopState = () => {
      const current = window.location.pathname.match(/^\/sessions\/([^/]+)$/)?.[1];
      if (current) void selectSession(decodeURIComponent(current));
      else newChat();
    };
    window.addEventListener("popstate", onPopState);
    return () => window.removeEventListener("popstate", onPopState);
  }, [newChat, refreshSessions, selectSession]);

  const submit = useCallback(async (requestedQuery = query, requestedEditSequence: number | null = editSequence) => {
    const text = requestedQuery.trim();
    if (!text || busy) return;

    const editing = requestedEditSequence !== null;
    const originalTurn = editing ? turns[requestedEditSequence] : undefined;
    const files = editing ? (originalTurn?.attached_files ?? []) : attachedFiles;
    setBusy(true);
    setError(null);
    if (editing) setTurns((current) => current.slice(0, requestedEditSequence));
    setPendingTurn({
      query: text,
      attached_files: files,
      session_id: activeSessionID ?? "",
      top_k: 0,
      generate: true,
      results: [],
      count: 0,
      elapsed_ms: 0,
      from_cache: false,
      has_answer: false,
      streaming: false,
    });
    setQuery("");
    setEditQuery("");
    setEditSequence(null);

    try {
      const turn = await startTurn({
        query: text,
        generate: true,
        session_id: activeSessionID ?? undefined,
        attached_files: files.length ? files : undefined,
        edit: editing,
        edit_sequence: requestedEditSequence ?? undefined,
      });
      const nextSessionID = turn.session_id || activeSessionID;
      if (nextSessionID && nextSessionID !== activeSessionID) {
        setActiveSessionID(nextSessionID);
        window.history.pushState({}, "", `/sessions/${encodeURIComponent(nextSessionID)}`);
      }
      if (!editing) setAttachedFiles([]);
      setPendingTurn(null);
      setTurns((current) => [...current, withSequence(turn, current.length)]);
      if (turn.streaming) {
        startStream(turn);
      } else {
        setBusy(false);
        void refreshSessions();
      }
    } catch (cause) {
      setPendingTurn(null);
      setBusy(false);
      setError(messageFrom(cause, "Could not start turn"));
    }
  }, [activeSessionID, attachedFiles, busy, editSequence, query, refreshSessions, startStream, turns]);

  const upload = useCallback(async (files: File[]) => {
    setUploading(true);
    setDocumentMessage(null);
    try {
      const response = await ingestDocuments(files);
      setAttachedFiles((current) => [...current, ...(response.names ?? files.map((file) => file.name))]);
      if (response.failed > 0) setDocumentMessage(`${response.processed} processed, ${response.skipped} skipped, ${response.failed} failed.`);
    } catch (cause) {
      setDocumentMessage(messageFrom(cause, "Import failed"));
    } finally {
      setUploading(false);
    }
  }, []);

  const confirmDeleteSession = useCallback(async () => {
    const target = deleteSessionTarget;
    if (!target) return;
    setDeleteSessionTarget(null);
    try {
      await deleteSession(target.id);
      if (activeSessionID === target.id) newChat();
      await refreshSessions();
    } catch (cause) {
      setError(messageFrom(cause, "Could not delete conversation"));
    }
  }, [activeSessionID, deleteSessionTarget, newChat, refreshSessions]);

  const removeAll = useCallback(async () => {
    if (deleteAllBusy) return;
    setDeleteAllBusy(true);
    setDeleteAllError("");
    try {
      await deleteAllSessions();
      setDeleteAllOpen(false);
      setDeleteAllBusy(false);
      newChat();
      setSessions([]);
    } catch (cause) {
      setDeleteAllBusy(false);
      setDeleteAllError(messageFrom(cause, "Could not delete chats. Please try again."));
    }
  }, [deleteAllBusy, newChat]);

  const editTurn = useCallback((turn: Turn, sequence: number) => {
    setEditQuery(turn.query);
    setEditSequence(sequence);
    window.setTimeout(() => {
      document.querySelector(`[data-turn-sequence="${sequence}"]`)?.scrollIntoView({ behavior: "smooth", block: "center" });
    }, 0);
  }, []);

  const stop = useCallback(() => {
    if (!activeTurnID) return;
    void cancelTurn(activeTurnID).finally(() => {
      closeStream();
      setBusy(false);
    });
  }, [activeTurnID, closeStream]);

  return (
    <div className="flex h-screen relative bg-[#f7f7f4]">
      {sidebarOpen && <div className="fixed inset-0 bg-black/30 z-30 md:hidden" onClick={() => setSidebarOpen(false)} />}

      {deleteSessionTarget && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/30 p-4" onMouseDown={(event) => { if (event.target === event.currentTarget) setDeleteSessionTarget(null); }}>
          <section className="bg-[#f7f7f4] border border-[#dcd8c9] rounded-[16px] shadow-xl w-full max-w-[340px] p-6 text-center">
            <div className="font-serif-display font-semibold text-[16px] mb-1">Delete chat?</div>
            <p className="text-[13px] text-[#8b8f81] mb-5 truncate">{deleteSessionTarget.title}</p>
            <div className="flex justify-center gap-2">
              <button type="button" onClick={() => setDeleteSessionTarget(null)} className="text-[13.5px] text-[#5c6156] border border-[#dcd8c9] rounded-lg px-3.5 py-2 hover:bg-[#eeece3] transition">Cancel</button>
              <button type="button" onClick={() => void confirmDeleteSession()} className="text-[13.5px] text-white bg-[#b04a3f] rounded-lg px-3.5 py-2 flex items-center gap-2 hover:bg-[#a03e34] transition">
                <TrashIcon />
                Delete
              </button>
            </div>
          </section>
        </div>
      )}

      <div className={`fixed inset-y-0 left-0 z-40 w-[264px] max-w-[82vw] transform transition-transform duration-200 ease-out md:static md:translate-x-0 md:flex-none ${sidebarOpen ? "translate-x-0" : "-translate-x-full"}`}>
        <SessionList
          sessions={sessions}
          activeSessionId={activeSessionID}
          onSelect={(id) => void selectSession(id)}
          onNew={newChat}
          onDelete={setDeleteSessionTarget}
          onSettings={() => setSettingsOpen(true)}
          onClose={() => setSidebarOpen(false)}
        />
      </div>

      <main className="flex flex-col min-w-0 min-h-0 flex-1">
        <div className="flex items-center gap-2.5 border-b border-[#e3e2d8] px-4 md:px-7 py-3 flex-none">
          <button type="button" onClick={() => setSidebarOpen((open) => !open)} aria-label="Toggle chats" className="md:hidden w-8 h-8 -ml-1 rounded-lg flex items-center justify-center text-[#5c6156] hover:bg-[#eeece3]">
            <svg width="18" height="18" viewBox="0 0 24 24" fill="none" aria-hidden="true"><path d="M3 6h18M3 12h18M3 18h18" stroke="currentColor" strokeWidth="2" strokeLinecap="round" /></svg>
          </button>
          <div className="font-serif-display font-semibold text-[17px]">New chat</div>
        </div>

        <div id="reader" ref={readerRef} className="flex-1 overflow-y-auto overflow-x-hidden">
          <div id="reader-inner" className="mx-auto max-w-[768px] px-4 md:px-7 pt-8 pb-4">
            {error && <div className="feedback feedback-err mb-4" role="alert">{error}</div>}
            {turns.length === 0 && !pendingTurn ? (
              <div id="empty-state" className="pt-16 text-center">
                <div className="font-serif-display text-[24px] font-semibold mb-2">Ask your documents</div>
                <p className="text-[14.5px] text-[#8b8f81] mb-6">Retrieval runs through Qdrant hybrid search before every generated answer.</p>
                <div className="flex flex-wrap justify-center gap-2">
                  <button type="button" onClick={() => void submit("What's the secant formula, and when do I use it instead of tangent?", null)} className="text-[14px] text-[#5c6156] border border-[#dcd8c9] rounded-lg px-3.5 py-2 hover:border-[#2f5d50] hover:text-[#234840] transition">What&apos;s the secant formula, and when do I use it instead of tangent?</button>
                  <button type="button" onClick={() => void submit("How does chunk overlap affect retrieval quality?", null)} className="text-[14px] text-[#5c6156] border border-[#dcd8c9] rounded-lg px-3.5 py-2 hover:border-[#2f5d50] hover:text-[#234840] transition">How does chunk overlap affect retrieval quality?</button>
                </div>
              </div>
            ) : (
              <>
                {turns.map((turn, index) => (
                  <TurnCard
                    key={`${turn.turn_id ?? turn.query}-${index}`}
                    turn={turn}
                    sequence={index}
                    editing={editSequence === index}
                    editQuery={editQuery}
                    onEdit={() => editTurn(turn, index)}
                    onEditQueryChange={setEditQuery}
                    onEditSubmit={() => void submit(editQuery, index)}
                    onCancelEdit={() => { setEditSequence(null); setEditQuery(""); }}
                  />
                ))}
                {pendingTurn && <TurnCard turn={pendingTurn} pending onEdit={() => {}} />}
              </>
            )}
          </div>
        </div>

        <Composer
          query={query}
          attachedFiles={attachedFiles}
          busy={busy}
          activeTurnID={activeTurnID}
          uploading={uploading}
          uploadMessage={documentMessage}
          onQueryChange={setQuery}
          onSubmit={() => void submit()}
          onStop={stop}
          onUpload={upload}
          onRemoveAttachment={(name) => setAttachedFiles((current) => current.filter((file) => file !== name))}
        />
      </main>

      {(settingsOpen || deleteAllOpen) && (
        <AdministrationPanel
          deleteAllOpen={deleteAllOpen}
          deleteAllBusy={deleteAllBusy}
          deleteAllError={deleteAllError}
          onClose={() => setSettingsOpen(false)}
          onOpenDeleteAll={() => { setSettingsOpen(false); setDeleteAllError(""); setDeleteAllOpen(true); }}
          onCloseDeleteAll={() => setDeleteAllOpen(false)}
          onDeleteAll={() => void removeAll()}
        />
      )}
    </div>
  );
}

function TrashIcon() {
  return <svg width="14" height="14" viewBox="0 0 24 24" fill="none" aria-hidden="true"><path d="M4 7h16M10 4h4M6 7l1 13a1 1 0 0 0 1 .93h8A1 1 0 0 0 17 20l1-13M10 11v6M14 11v6" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" /></svg>;
}
