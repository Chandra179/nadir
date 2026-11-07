package documents

import "nadir/internal/providers/cloudflare/d1"

// The D1 tables are the source of truth for what is visible. Vectorize holds
// only vectors, found by nearest-neighbour search and then resolved here, so a
// vector whose version is not active is simply dropped from results.
//
//   - doc_state.generation is the published corpus generation. Reset publishes a
//     new generation with one UPDATE; older generations are cleaned afterwards.
//   - doc_versions is the manifest: one row per (path, sha), active or staged.
//   - doc_chunks holds chunk text; seq is its rowid and the FTS rowid, so keyword
//     rows are written, found and deleted by primary key.
//   - doc_fts is the BM25 keyword leg (FTS5), indexing the contextual text.
var schema = []d1.Statement{
	{SQL: `CREATE TABLE IF NOT EXISTS doc_state (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		generation INTEGER NOT NULL
	)`},
	{SQL: `INSERT OR IGNORE INTO doc_state (id, generation) VALUES (1, 1)`},
	{SQL: `CREATE TABLE IF NOT EXISTS doc_versions (
		generation INTEGER NOT NULL,
		file_path TEXT NOT NULL,
		source_sha TEXT NOT NULL,
		chunk_count INTEGER NOT NULL,
		active INTEGER NOT NULL,
		PRIMARY KEY (generation, file_path, source_sha)
	)`},
	{SQL: `CREATE TABLE IF NOT EXISTS doc_chunks (
		seq INTEGER PRIMARY KEY,
		id TEXT NOT NULL UNIQUE,
		generation INTEGER NOT NULL,
		file_path TEXT NOT NULL,
		source_sha TEXT NOT NULL,
		line_start INTEGER NOT NULL,
		chunk_index INTEGER NOT NULL,
		header TEXT NOT NULL,
		section_path TEXT NOT NULL,
		text TEXT NOT NULL,
		window_text TEXT NOT NULL,
		ingested_at TEXT NOT NULL
	)`},
	{SQL: `CREATE INDEX IF NOT EXISTS doc_chunks_version ON doc_chunks (generation, file_path, source_sha)`},
	// remove_diacritics 0 keeps accented letters distinct, as the keyword
	// tokenizer shared with the Qdrant store does.
	{SQL: `CREATE VIRTUAL TABLE IF NOT EXISTS doc_fts USING fts5(sparse_text, tokenize = 'unicode61 remove_diacritics 0')`},
}

const currentGeneration = `(SELECT generation FROM doc_state WHERE id = 1)`

const chunkColumns = `c.id, c.file_path, c.source_sha, c.line_start, c.chunk_index, c.header, c.section_path, c.text, c.window_text`

const activeJoin = `JOIN doc_versions v ON v.generation = c.generation AND v.file_path = c.file_path AND v.source_sha = c.source_sha`
