// HTTP DTOs kept in one module so feature packages cannot silently drift apart.

export type Result = {
  file_path: string;
  header?: string;
  line_start: number;
  chunk_index?: number;
  retrieval_rank?: number;
  score: number;
  source_sha?: string;
  text: string;
};

export type Citation = {
  number: number;
  retrieval_rank: number;
  file_path: string;
  header?: string;
  line_start: number;
  chunk_index: number;
  source_sha?: string;
  text: string;
  truncated?: boolean;
};

export type Filter = {
  file_path?: string;
  header?: string;
  source_sha?: string;
};

export type Turn = {
  operation_id?: string;
  error?: string;
  query: string;
  rewritten_query?: string;
  attached_files?: string[];
  session_id: string;
  sequence?: number;
  top_k: number;
  generate: boolean;
  results: Result[];
  citations?: Citation[];
  count: number;
  elapsed_ms: number;
  from_cache: boolean;
  answer?: string;
  has_answer: boolean;
  turn_id?: string;
  stream_url?: string;
  prompt?: string;
  generate_error?: string;
  streaming: boolean;
};

export type StartTurnRequest = {
  query: string;
  top_k?: number;
  filter?: Filter;
  generate: boolean;
  session_id?: string;
  attached_files?: string[];
  edit?: boolean;
  edit_sequence?: number;
};

export type Session = {
  id: string;
  title: string;
  created_at: string;
  updated_at: string;
  turn_count: number;
};

export type SessionsResponse = {
  enabled: boolean;
  sessions: Session[];
};

export type SessionDetail = {
  session: Session;
  turns: Turn[];
};

export type IngestResponse = {
  operation_id?: string;
  processed: number;
  skipped: number;
  failed: number;
  removed: number;
  names?: string[];
  files?: { name: string; status: "processed" | "skipped" | "failed"; error?: string; published?: boolean }[];
  error?: string;
};

export type DocumentsResponse = {
  documents: { file_path: string; source_sha: string }[];
  count: number;
  last_import?: { completed_at: string; result: IngestResponse };
  last_import_scope: "since_process_start";
};

export type DeleteResponse = {
  deleted: boolean;
  error?: string;
};
