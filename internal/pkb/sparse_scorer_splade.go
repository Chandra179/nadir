package pkb

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

// SPLADESparseScorer implements SparseScorer via a SPLADE sidecar HTTP service.
// The sidecar returns sparse vectors (indices + values); scoring is dot product.
// Formula: score = Σ q_i * d_i over shared vocabulary indices.
// See: Formal et al. 2021 "SPLADE: Sparse Lexical and Expansion Model" (arxiv 2107.05720).
type SPLADESparseScorer struct {
	addr   string // e.g. http://localhost:5001
	client *http.Client
}

type sparseVector struct {
	Indices []int     `json:"indices"`
	Values  []float64 `json:"values"`
}

type spladeRequest struct {
	Text string `json:"text"`
	Type string `json:"type"` // "query" or "passage"
}

func NewSPLADESparseScorer(addr string) *SPLADESparseScorer {
	return &SPLADESparseScorer{
		addr: addr,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (s *SPLADESparseScorer) Score(query, text string) float64 {
	ctx := context.Background()
	qVec, err := s.embed(ctx, query, "query")
	if err != nil {
		log.Printf("splade: embed query failed (sidecar down?): %v", err)
		return 0
	}
	dVec, err := s.embed(ctx, text, "passage")
	if err != nil {
		log.Printf("splade: embed passage failed: %v", err)
		return 0
	}
	return dotProduct(qVec, dVec)
}

// EmbedSparse implements SparseEmbedder. embedType: "query" or "passage".
func (s *SPLADESparseScorer) EmbedSparse(ctx context.Context, text, embedType string) ([]uint32, []float32, error) {
	vec, err := s.embed(ctx, text, embedType)
	if err != nil {
		return nil, nil, err
	}
	indices := make([]uint32, len(vec.Indices))
	for i, idx := range vec.Indices {
		indices[i] = uint32(idx)
	}
	values := make([]float32, len(vec.Values))
	for i, v := range vec.Values {
		values[i] = float32(v)
	}
	return indices, values, nil
}

func (s *SPLADESparseScorer) embed(ctx context.Context, text, embedType string) (sparseVector, error) {
	body, _ := json.Marshal(spladeRequest{Text: text, Type: embedType})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.addr+"/embed_sparse", bytes.NewReader(body))
	if err != nil {
		return sparseVector{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return sparseVector{}, fmt.Errorf("splade embed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return sparseVector{}, fmt.Errorf("splade embed: status %d", resp.StatusCode)
	}

	var vec sparseVector
	if err := json.NewDecoder(resp.Body).Decode(&vec); err != nil {
		return sparseVector{}, fmt.Errorf("splade decode: %w", err)
	}
	return vec, nil
}

func dotProduct(q, d sparseVector) float64 {
	dMap := make(map[int]float64, len(d.Indices))
	for i, idx := range d.Indices {
		dMap[idx] = d.Values[i]
	}
	var sum float64
	for i, idx := range q.Indices {
		if val, ok := dMap[idx]; ok {
			sum += q.Values[i] * val
		}
	}
	return sum
}
