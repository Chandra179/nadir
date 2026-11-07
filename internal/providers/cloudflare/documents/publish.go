package documents

import (
	"context"
	"fmt"
	"strings"
	"time"

	indexing "nadir/internal/core/documents/indexing"
	"nadir/internal/providers/cloudflare/d1"
	"nadir/internal/providers/cloudflare/vectorize"
	"nadir/internal/providers/lexical"
)

// ReplaceDocument is a failure-safe, versioned replacement. The new version is
// staged invisibly (inactive manifest row, chunk rows, keyword rows, vectors),
// then one atomic D1 batch activates it and retires every other version of the
// path. Cleanup of the retired rows and vectors follows; a failure there is a
// *PublicationError because the new version is already visible.
func (s *dependencies) ReplaceDocument(ctx context.Context, filePath, sourceSHA string, chunks []indexing.IndexedChunk) error {
	if strings.TrimSpace(filePath) == "" {
		return fmt.Errorf("document file path is required")
	}
	if strings.TrimSpace(sourceSHA) == "" {
		return fmt.Errorf("document source SHA is required")
	}
	for _, c := range chunks {
		if c.FilePath != filePath {
			return fmt.Errorf("document chunk path %q does not match %q", c.FilePath, filePath)
		}
		if c.SourceSHA != sourceSHA {
			return fmt.Errorf("document chunk source SHA %q does not match %q", c.SourceSHA, sourceSHA)
		}
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	if err := s.notReady(); err != nil {
		return err
	}
	// Concurrent callers must not both observe an unpublished version and then
	// restage it after the other has activated it.
	s.publicationMu.Lock()
	defer s.publicationMu.Unlock()

	generation, err := s.generation(ctx)
	if err != nil {
		return err
	}

	// An activated version is complete and immutable: a retry after a cleanup
	// failure must not restage it.
	rows, err := s.db.Exec(ctx, d1.Statement{
		SQL:    `SELECT chunk_count FROM doc_versions WHERE generation = ? AND file_path = ? AND source_sha = ? AND active = 1`,
		Params: []any{generation, filePath, sourceSHA},
	})
	if err != nil {
		return fmt.Errorf("check published document version: %w", err)
	}
	published := len(rows[0]) > 0
	if published && int(rows[0][0].Int("chunk_count")) != len(chunks) {
		return fmt.Errorf("published document version contains %d chunks, expected %d; reset and reindex to change an existing version", rows[0][0].Int("chunk_count"), len(chunks))
	}

	if !published && len(chunks) > 0 {
		if err := s.stage(ctx, generation, filePath, sourceSHA, chunks); err != nil {
			return fmt.Errorf("stage document version: %w", err)
		}
	}

	// Activation and retirement are one transaction: the old version stays
	// visible until this batch applies, then the new one replaces it.
	var flip []d1.Statement
	if len(chunks) == 0 {
		// Empty content publishes absence: retire every version of the path
		// and do not activate abandoned staging for this SHA.
		flip = []d1.Statement{{
			SQL:    `UPDATE doc_versions SET active = 0 WHERE generation = ? AND file_path = ?`,
			Params: []any{generation, filePath},
		}}
	} else {
		flip = []d1.Statement{
			{SQL: `UPDATE doc_versions SET active = 1 WHERE generation = ? AND file_path = ? AND source_sha = ?`, Params: []any{generation, filePath, sourceSHA}},
			{SQL: `UPDATE doc_versions SET active = 0 WHERE generation = ? AND file_path = ? AND source_sha <> ?`, Params: []any{generation, filePath, sourceSHA}},
		}
	}
	if _, err := s.db.Exec(ctx, flip...); err != nil {
		mutationErr := fmt.Errorf("activate document version: %w", err)
		if published {
			return &indexing.PublicationError{Err: mutationErr}
		}
		return mutationErr
	}

	if err := s.deleteInactive(ctx, generation, filePath); err != nil {
		return &indexing.PublicationError{Err: fmt.Errorf("delete previous document versions: %w", err)}
	}
	return nil
}

// stage writes one version invisibly. D1 rows go first so that a crash part
// way through leaves rows from which a later cleanup can find every vector id.
func (s *dependencies) stage(ctx context.Context, generation int64, filePath, sourceSHA string, chunks []indexing.IndexedChunk) error {
	// A retried staging replaces any earlier attempt for the same version, even
	// one that was chunked differently.
	if err := s.removeVersion(ctx, generation, filePath, sourceSHA); err != nil {
		return err
	}
	if _, err := s.db.Exec(ctx, d1.Statement{
		SQL:    `INSERT OR REPLACE INTO doc_versions (generation, file_path, source_sha, chunk_count, active) VALUES (?, ?, ?, ?, 0)`,
		Params: []any{generation, filePath, sourceSHA, len(chunks)},
	}); err != nil {
		return fmt.Errorf("record version: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	for start := 0; start < len(chunks); start += chunksPerBatch {
		end := min(start+chunksPerBatch, len(chunks))
		var statements []d1.Statement
		for _, c := range chunks[start:end] {
			id := chunkID(c, generation)
			ingestedAt := c.IngestedAt
			if ingestedAt == "" {
				ingestedAt = now
			}
			sparse := c.SparseText
			if sparse == "" {
				sparse = lexical.ContextualText(c.FilePath, c.Header, c.Text)
			}
			statements = append(statements,
				d1.Statement{
					SQL: `INSERT OR IGNORE INTO doc_chunks (id, generation, file_path, source_sha, line_start, chunk_index, header, section_path, text, window_text, ingested_at)
						VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
					Params: []any{id, generation, c.FilePath, c.SourceSHA, c.LineStart, c.ChunkIndex, c.Header, c.SectionPath, c.Text, c.WindowText, ingestedAt},
				},
				d1.Statement{
					SQL:    `INSERT INTO doc_fts (rowid, sparse_text) SELECT seq, ? FROM doc_chunks WHERE id = ? AND NOT EXISTS (SELECT 1 FROM doc_fts WHERE rowid = doc_chunks.seq)`,
					Params: []any{sparse, id},
				},
			)
		}
		if _, err := s.db.Exec(ctx, statements...); err != nil {
			return fmt.Errorf("write chunks: %w", err)
		}
	}

	var lastMutation string
	for start := 0; start < len(chunks); start += vectorsPerBatch {
		end := min(start+vectorsPerBatch, len(chunks))
		vectors := make([]vectorize.Vector, 0, end-start)
		for _, c := range chunks[start:end] {
			vectors = append(vectors, vectorize.Vector{
				ID:     chunkID(c, generation),
				Values: c.Vector,
				Metadata: map[string]string{
					metaFile:   metaKey(c.FilePath),
					metaHeader: metaKey(c.Header),
					metaSHA:    metaKey(c.SourceSHA),
				},
			})
		}
		id, err := s.index.UpsertMany(ctx, vectors)
		if err != nil {
			return fmt.Errorf("write vectors: %w", err)
		}
		lastMutation = id
	}
	// Mutations are applied in order, so the last one covers the rest. Giving up
	// the wait only delays the dense leg for this document; it is not an error.
	if _, err := s.index.WaitForMutation(ctx, lastMutation, mutationWait); err != nil {
		return fmt.Errorf("wait for vectors: %w", err)
	}
	return nil
}

// removeVersion deletes the rows and vectors of one (path, sha) version.
func (s *dependencies) removeVersion(ctx context.Context, generation int64, filePath, sourceSHA string) error {
	ids, err := s.chunkIDs(ctx, d1.Statement{
		SQL:    `SELECT id FROM doc_chunks WHERE generation = ? AND file_path = ? AND source_sha = ?`,
		Params: []any{generation, filePath, sourceSHA},
	})
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	if err := s.index.DeleteByIDs(ctx, ids); err != nil {
		return fmt.Errorf("delete vectors: %w", err)
	}
	_, err = s.db.Exec(ctx,
		d1.Statement{SQL: `DELETE FROM doc_fts WHERE rowid IN (SELECT seq FROM doc_chunks WHERE generation = ? AND file_path = ? AND source_sha = ?)`, Params: []any{generation, filePath, sourceSHA}},
		d1.Statement{SQL: `DELETE FROM doc_chunks WHERE generation = ? AND file_path = ? AND source_sha = ?`, Params: []any{generation, filePath, sourceSHA}},
	)
	if err != nil {
		return fmt.Errorf("delete rows: %w", err)
	}
	return nil
}

// deleteInactive removes every non-active version of a path: vectors first, so
// a failure leaves rows from which a retry rediscovers the ids.
func (s *dependencies) deleteInactive(ctx context.Context, generation int64, filePath string) error {
	const inactive = `generation = ? AND file_path = ? AND NOT EXISTS (
		SELECT 1 FROM doc_versions v WHERE v.generation = doc_chunks.generation AND v.file_path = doc_chunks.file_path
		AND v.source_sha = doc_chunks.source_sha AND v.active = 1)`
	ids, err := s.chunkIDs(ctx, d1.Statement{SQL: `SELECT id FROM doc_chunks WHERE ` + inactive, Params: []any{generation, filePath}})
	if err != nil {
		return err
	}
	if len(ids) > 0 {
		if err := s.index.DeleteByIDs(ctx, ids); err != nil {
			return fmt.Errorf("delete vectors: %w", err)
		}
	}
	_, err = s.db.Exec(ctx,
		d1.Statement{SQL: `DELETE FROM doc_fts WHERE rowid IN (SELECT seq FROM doc_chunks WHERE ` + inactive + `)`, Params: []any{generation, filePath}},
		d1.Statement{SQL: `DELETE FROM doc_chunks WHERE ` + inactive, Params: []any{generation, filePath}},
		d1.Statement{SQL: `DELETE FROM doc_versions WHERE generation = ? AND file_path = ? AND active = 0`, Params: []any{generation, filePath}},
	)
	if err != nil {
		return fmt.Errorf("delete rows: %w", err)
	}
	return nil
}

func (s *dependencies) chunkIDs(ctx context.Context, statement d1.Statement) ([]string, error) {
	rows, err := s.db.Exec(ctx, statement)
	if err != nil {
		return nil, fmt.Errorf("list chunk ids: %w", err)
	}
	ids := make([]string, len(rows[0]))
	for i, row := range rows[0] {
		ids[i] = row.Str("id")
	}
	return ids, nil
}

// DeleteDocument removes every stored version of one source path. The manifest
// rows go first, which hides the document at once; vectors and rows follow.
func (s *dependencies) DeleteDocument(ctx context.Context, filePath string) error {
	if strings.TrimSpace(filePath) == "" {
		return fmt.Errorf("document file path is required")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if err := s.notReady(); err != nil {
		return err
	}
	s.publicationMu.Lock()
	defer s.publicationMu.Unlock()

	generation, err := s.generation(ctx)
	if err != nil {
		return err
	}
	if _, err := s.db.Exec(ctx, d1.Statement{SQL: `DELETE FROM doc_versions WHERE generation = ? AND file_path = ?`, Params: []any{generation, filePath}}); err != nil {
		return fmt.Errorf("delete document %q: %w", filePath, err)
	}
	ids, err := s.chunkIDs(ctx, d1.Statement{SQL: `SELECT id FROM doc_chunks WHERE generation = ? AND file_path = ?`, Params: []any{generation, filePath}})
	if err != nil {
		return fmt.Errorf("delete document %q: %w", filePath, err)
	}
	if len(ids) > 0 {
		if err := s.index.DeleteByIDs(ctx, ids); err != nil {
			return fmt.Errorf("delete document %q: delete vectors: %w", filePath, err)
		}
	}
	if _, err := s.db.Exec(ctx,
		d1.Statement{SQL: `DELETE FROM doc_fts WHERE rowid IN (SELECT seq FROM doc_chunks WHERE generation = ? AND file_path = ?)`, Params: []any{generation, filePath}},
		d1.Statement{SQL: `DELETE FROM doc_chunks WHERE generation = ? AND file_path = ?`, Params: []any{generation, filePath}},
	); err != nil {
		return fmt.Errorf("delete document %q: delete rows: %w", filePath, err)
	}
	return nil
}

// DeleteAll resets the corpus. Publishing the new, empty generation is a single
// UPDATE: until it applies the old corpus stays fully searchable, and afterwards
// the old one is invisible. Retiring the old generation's vectors and rows then
// follows; a failure there is a *PublicationError and can be retried.
func (s *dependencies) DeleteAll(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.notReady(); err != nil {
		return fmt.Errorf("document collection dimensions are not initialized")
	}
	rows, err := s.db.Exec(ctx,
		d1.Statement{SQL: `UPDATE doc_state SET generation = generation + 1 WHERE id = 1`},
		d1.Statement{SQL: `SELECT generation FROM doc_state WHERE id = 1`},
	)
	if err != nil {
		return fmt.Errorf("publish document generation: %w", err)
	}
	if len(rows[1]) == 0 {
		return fmt.Errorf("publish document generation: no generation row")
	}
	newest := rows[1][0].Int("generation")

	for {
		ids, err := s.chunkIDs(ctx, d1.Statement{SQL: `SELECT id FROM doc_chunks WHERE generation < ? LIMIT ?`, Params: []any{newest, cleanupPage}})
		if err != nil {
			return &indexing.PublicationError{Err: fmt.Errorf("reset published but cleanup failed: %w", err)}
		}
		if len(ids) == 0 {
			break
		}
		if err := s.index.DeleteByIDs(ctx, ids); err != nil {
			return &indexing.PublicationError{Err: fmt.Errorf("reset published but could not delete retired vectors: %w", err)}
		}
		for start := 0; start < len(ids); start += idsPerStatement {
			batch := ids[start:min(start+idsPerStatement, len(ids))]
			params := make([]any, len(batch))
			for i, id := range batch {
				params[i] = id
			}
			marks := strings.TrimSuffix(strings.Repeat("?,", len(batch)), ",")
			if _, err := s.db.Exec(ctx,
				d1.Statement{SQL: `DELETE FROM doc_fts WHERE rowid IN (SELECT seq FROM doc_chunks WHERE id IN (` + marks + `))`, Params: params},
				d1.Statement{SQL: `DELETE FROM doc_chunks WHERE id IN (` + marks + `)`, Params: params},
			); err != nil {
				return &indexing.PublicationError{Err: fmt.Errorf("reset published but could not delete retired rows: %w", err)}
			}
		}
	}
	if _, err := s.db.Exec(ctx, d1.Statement{SQL: `DELETE FROM doc_versions WHERE generation < ?`, Params: []any{newest}}); err != nil {
		return &indexing.PublicationError{Err: fmt.Errorf("reset published but could not delete retired versions: %w", err)}
	}
	return nil
}

// GetAllFileSHAs returns the active source SHA of every published document.
func (s *dependencies) GetAllFileSHAs(ctx context.Context) (map[string]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if err := s.notReady(); err != nil {
		return nil, err
	}
	shas := make(map[string]string)
	after := ""
	for {
		rows, err := s.db.Exec(ctx, d1.Statement{
			SQL: `SELECT file_path, source_sha FROM doc_versions
				WHERE generation = ` + currentGeneration + ` AND active = 1 AND file_path > ? ORDER BY file_path LIMIT ?`,
			Params: []any{after, shaPageSize},
		})
		if err != nil {
			return nil, fmt.Errorf("list file shas: %w", err)
		}
		for _, row := range rows[0] {
			shas[row.Str("file_path")] = row.Str("source_sha")
			after = row.Str("file_path")
		}
		if len(rows[0]) < shaPageSize {
			return shas, nil
		}
	}
}
