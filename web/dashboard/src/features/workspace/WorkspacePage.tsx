import { useCallback, useEffect, useRef, useState } from "react";

import Notice from "../../components/Notice";
import type { Session, Turn } from "../../lib/api-contract";
import { useTurnStream } from "../../hooks/useTurnStream";
import AdministrationPanel from "../administration/AdministrationPanel";
import { cancelTurn, startTurn } from "../chat/api";
import Composer from "../chat/Composer";
import TurnCard from "../chat/TurnCard";
import { ingestDocuments, resetDocuments } from "../documents/api";
import DocumentPanel from "../documents/DocumentPanel";
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
  const [query, setQuery] = useState("");
  const [editSequence, setEditSequence] = useState<number | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [uploading, setUploading] = useState(false);
  const [documentMessage, setDocumentMessage] = useState<string | null>(null);
  const [attachedFiles, setAttachedFiles] = useState<string[]>([]);
  const turnsEndRef = useRef<HTMLDivElement | null>(null);

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
    turnsEndRef.current?.scrollIntoView({ block: "end" });
  }, [turns]);

  const selectSession = useCallback(async (id: string) => {
    closeStream();
    setError(null);
    try {
      const detail = await getSession(id);
      setActiveSessionID(id);
      setTurns(detail.turns.map(withSequence));
      window.history.pushState({}, "", `/sessions/${encodeURIComponent(id)}`);
    } catch (cause) {
      setError(messageFrom(cause, "Could not load conversation"));
    }
  }, [closeStream]);

  const newChat = useCallback(() => {
    closeStream();
    setActiveSessionID(null);
    setTurns([]);
    setQuery("");
    setEditSequence(null);
    setAttachedFiles([]);
    setError(null);
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

  const submit = useCallback(async () => {
    const text = query.trim();
    if (!text || busy) return;

    const editing = editSequence !== null;
    const sequence = editSequence;
    setBusy(true);
    setError(null);
    if (editing && sequence !== null) {
      setTurns((current) => current.slice(0, sequence));
    }
    setQuery("");
    setEditSequence(null);

    try {
      const turn = await startTurn({
        query: text,
        generate: true,
        session_id: activeSessionID ?? undefined,
        attached_files: attachedFiles.length ? attachedFiles : undefined,
        edit: editing,
        edit_sequence: sequence ?? undefined,
      });
      const nextSessionID = turn.session_id || activeSessionID;
      if (nextSessionID && nextSessionID !== activeSessionID) {
        setActiveSessionID(nextSessionID);
        window.history.pushState({}, "", `/sessions/${encodeURIComponent(nextSessionID)}`);
      }
      setAttachedFiles([]);
      setTurns((current) => [...current, withSequence(turn, current.length)]);
      if (turn.streaming) {
        startStream(turn);
      } else {
        setBusy(false);
        void refreshSessions();
      }
    } catch (cause) {
      setBusy(false);
      setError(messageFrom(cause, "Could not start turn"));
    }
  }, [activeSessionID, attachedFiles, busy, editSequence, query, refreshSessions, startStream]);

  const upload = useCallback(async (files: File[]) => {
    setUploading(true);
    setDocumentMessage(null);
    try {
      const response = await ingestDocuments(files);
      setAttachedFiles((current) => [...current, ...(response.names ?? files.map((file) => file.name))]);
      setDocumentMessage(`${response.processed} processed, ${response.skipped} skipped, ${response.failed} failed.`);
    } catch (cause) {
      setDocumentMessage(messageFrom(cause, "Upload failed"));
    } finally {
      setUploading(false);
    }
  }, []);

  const removeOne = useCallback(async (session: Session) => {
    if (!window.confirm(`Delete “${session.title}” and all its turns?`)) return;
    try {
      await deleteSession(session.id);
      if (activeSessionID === session.id) newChat();
      await refreshSessions();
    } catch (cause) {
      setError(messageFrom(cause, "Could not delete conversation"));
    }
  }, [activeSessionID, newChat, refreshSessions]);

  const removeAll = useCallback(async () => {
    if (!window.confirm("Delete every conversation? This cannot be undone.")) return;
    try {
      await deleteAllSessions();
      newChat();
      setSessions([]);
      setSettingsOpen(false);
    } catch (cause) {
      setError(messageFrom(cause, "Could not delete conversations"));
    }
  }, [newChat]);

  const resetIndex = useCallback(async () => {
    try {
      await resetDocuments();
      setDocumentMessage("Document index reset.");
    } catch (cause) {
      setDocumentMessage(messageFrom(cause, "Could not reset document index"));
    }
  }, []);

  const editTurn = useCallback((turn: Turn, sequence: number) => {
    setQuery(turn.query);
    setEditSequence(sequence);
    window.scrollTo({ top: document.body.scrollHeight, behavior: "smooth" });
  }, []);

  const stop = useCallback(() => {
    if (!activeTurnID) return;
    void cancelTurn(activeTurnID).finally(() => {
      closeStream();
      setBusy(false);
    });
  }, [activeTurnID, closeStream]);

  return (
    <div className="flex min-h-screen flex-col bg-nadir-ink text-slate-200 md:flex-row">
      <SessionList
        sessions={sessions}
        activeSessionId={activeSessionID}
        onSelect={(id) => void selectSession(id)}
        onNew={newChat}
        onDelete={(session) => void removeOne(session)}
      />
      <main className="flex min-w-0 flex-1 flex-col">
        <header className="flex items-center justify-between border-b border-slate-800 px-5 py-4 md:px-10">
          <div>
            <p className="text-sm font-medium text-slate-300">
              {activeSessionID ? "Conversation" : "New conversation"}
            </p>
            <p className="text-xs text-slate-600">Search your indexed knowledge with grounded context.</p>
          </div>
          <button
            onClick={() => setSettingsOpen(true)}
            className="rounded-lg border border-slate-700 px-3 py-2 text-xs text-slate-400 transition hover:border-slate-500 hover:text-slate-200"
          >
            Settings
          </button>
        </header>

        <div className="scrollbar-thin flex-1 overflow-y-auto">
          <div className="mx-auto flex w-full max-w-4xl flex-col gap-6 px-5 py-8 md:px-10">
            {error && <Notice tone="error">{error}</Notice>}
            {turns.length === 0 && (
              <div className="rounded-3xl border border-dashed border-slate-800 px-6 py-20 text-center">
                <p className="text-lg text-slate-300">Ask Nadir about your documents.</p>
                <p className="mx-auto mt-2 max-w-md text-sm leading-6 text-slate-600">
                  Upload source material, then ask a question. Answers stream from retrieved passages and remain
                  attached to this conversation.
                </p>
              </div>
            )}
            {turns.map((turn, index) => (
              <TurnCard
                key={`${turn.turn_id ?? turn.query}-${index}`}
                turn={turn}
                onEdit={() => editTurn(turn, index)}
              />
            ))}
            <DocumentPanel onUpload={upload} onReset={resetIndex} uploading={uploading} message={documentMessage} />
            <div ref={turnsEndRef} />
          </div>
        </div>

        <Composer
          query={query}
          attachedFiles={attachedFiles}
          editSequence={editSequence}
          busy={busy}
          activeTurnID={activeTurnID}
          onQueryChange={setQuery}
          onSubmit={() => void submit()}
          onCancelEdit={() => {
            setEditSequence(null);
            setQuery("");
          }}
          onStop={stop}
        />
      </main>

      {settingsOpen && <AdministrationPanel onClose={() => setSettingsOpen(false)} onDeleteAll={() => void removeAll()} />}
    </div>
  );
}
