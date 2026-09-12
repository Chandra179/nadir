// HTTP DTOs mirrored from contracts/http/openapi.yaml. Keeping transport
// shapes in one module prevents feature packages from silently drifting apart.

export type Result = {
  file_path: string;
  header?: string;
  line_start: number;
  score: number;
  source_sha?: string;
  text: string;
};

export type Turn = {
  error?: string;
  query: string;
  rewritten_query?: string;
  attached_files?: string[];
  session_id: string;
  sequence?: number;
  top_k: number;
  generate: boolean;
  results: Result[];
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
  processed: number;
  skipped: number;
  failed: number;
  names?: string[];
  error?: string;
};
