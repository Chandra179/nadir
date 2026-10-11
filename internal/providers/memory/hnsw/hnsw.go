// Package hnsw is a small in-memory Hierarchical Navigable Small World index
// for cosine similarity (Malkov and Yashunin, 2016). It is pure Go so it builds
// for js/wasm. Levels come from a seeded generator and ties break on node
// order, so the same inserts always build the same graph.
//
// An Index is not safe for concurrent mutation. Search is read-only, so many
// searches may run together as long as nothing calls Add or Remove meanwhile.
package hnsw

import (
	"cmp"
	"container/heap"
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
)

// Config tunes the graph. Zero values select the defaults.
type Config struct {
	// M is the number of links kept per node on upper layers (2*M on layer 0).
	M int
	// EfConstruction is the candidate list size while inserting.
	EfConstruction int
	// Seed makes level assignment reproducible.
	Seed uint64
}

// Hit is one search result. Sim is the cosine similarity.
type Hit struct {
	ID  uint32
	Sim float32
}

type node struct {
	id      uint32
	vec     []float32 // unit length (or all zero)
	links   [][]int32 // per layer, indexes into Index.nodes
	deleted bool
}

// Index is the graph. The zero value is not usable; call New.
type Index struct {
	cfg      Config
	mL       float64
	rng      *rand.Rand
	dims     int
	nodes    []*node
	byID     map[uint32]int32
	entry    int32
	maxLevel int
	deleted  int
}

// New returns an empty index.
func New(cfg Config) *Index {
	if cfg.M <= 1 {
		cfg.M = 16
	}
	if cfg.EfConstruction <= 0 {
		cfg.EfConstruction = 200
	}
	return &Index{
		cfg:   cfg,
		mL:    1 / math.Log(float64(cfg.M)),
		rng:   rand.New(rand.NewPCG(cfg.Seed, cfg.Seed^0x9e3779b97f4a7c15)),
		byID:  map[uint32]int32{},
		entry: -1,
	}
}

// Len is the number of live (not removed) vectors.
func (ix *Index) Len() int { return len(ix.nodes) - ix.deleted }

// NeedsRebuild reports that removed nodes make up enough of the graph that a
// fresh index over the live vectors would be smaller and faster.
func (ix *Index) NeedsRebuild() bool {
	return len(ix.nodes) >= 64 && ix.deleted*10 > len(ix.nodes)*3
}

// Add inserts a vector under id. The vector is normalized; when it is already
// unit length the slice is kept without copying, so the caller must not modify
// it afterwards.
func (ix *Index) Add(id uint32, vec []float32) error {
	if len(vec) == 0 {
		return fmt.Errorf("hnsw: empty vector")
	}
	if !finite(vec) {
		return fmt.Errorf("hnsw: vector has a NaN or infinite component")
	}
	if ix.dims == 0 {
		ix.dims = len(vec)
	} else if len(vec) != ix.dims {
		return fmt.Errorf("hnsw: vector has %d dimensions, want %d", len(vec), ix.dims)
	}
	if _, ok := ix.byID[id]; ok {
		return fmt.Errorf("hnsw: id %d already present", id)
	}

	unit := normalized(vec)
	level := int(-math.Log(1-ix.rng.Float64()) * ix.mL)
	idx := int32(len(ix.nodes))
	ix.nodes = append(ix.nodes, &node{id: id, vec: unit, links: make([][]int32, level+1)})
	ix.byID[id] = idx
	if ix.entry < 0 {
		ix.entry, ix.maxLevel = idx, level
		return nil
	}

	eps := []item{{ix.entry, dot(unit, ix.nodes[ix.entry].vec)}}
	for l := ix.maxLevel; l > level; l-- {
		eps = ix.searchLayer(unit, eps, 1, l, true)
	}
	for l := min(level, ix.maxLevel); l >= 0; l-- {
		cands := ix.searchLayer(unit, eps, ix.cfg.EfConstruction, l, true)
		maxLinks := ix.cfg.M
		if l == 0 {
			maxLinks = 2 * ix.cfg.M
		}
		chosen := ix.selectNeighbors(cands, ix.cfg.M)
		links := make([]int32, len(chosen))
		for i, c := range chosen {
			links[i] = c.idx
			ix.link(c.idx, idx, l, maxLinks)
		}
		ix.nodes[idx].links[l] = links
		eps = cands
	}
	if level > ix.maxLevel {
		ix.entry, ix.maxLevel = idx, level
	}
	return nil
}

// Remove hides id from results. Its node stays in the graph as a connector
// until the index is rebuilt. Unknown ids are ignored.
func (ix *Index) Remove(id uint32) {
	idx, ok := ix.byID[id]
	if !ok || ix.nodes[idx].deleted {
		return
	}
	ix.nodes[idx].deleted = true
	ix.deleted++
	delete(ix.byID, id)
}

// Search returns up to k nearest live vectors, best first. ef is the search
// breadth: larger is slower and closer to exact. Results with equal similarity
// are ordered by id.
func (ix *Index) Search(query []float32, k, ef int) []Hit {
	if k <= 0 || ix.entry < 0 || len(query) != ix.dims || ix.Len() == 0 || !finite(query) {
		return nil
	}
	ef = max(ef, k)
	q := normalized(query)
	eps := []item{{ix.entry, dot(q, ix.nodes[ix.entry].vec)}}
	for l := ix.maxLevel; l > 0; l-- {
		eps = ix.searchLayer(q, eps, 1, l, true)
	}
	found := ix.searchLayer(q, eps, ef, 0, false)
	hits := make([]Hit, 0, min(k, len(found)))
	for _, f := range found {
		hits = append(hits, Hit{ID: ix.nodes[f.idx].id, Sim: f.sim})
	}
	slices.SortFunc(hits, func(a, b Hit) int {
		if c := cmp.Compare(b.Sim, a.Sim); c != 0 {
			return c
		}
		return cmp.Compare(a.ID, b.ID)
	})
	if len(hits) > k {
		hits = hits[:k]
	}
	return hits
}

// link adds the edge from -> to on a layer, pruning to maxLinks.
func (ix *Index) link(from, to int32, layer, maxLinks int) {
	n := ix.nodes[from]
	n.links[layer] = append(n.links[layer], to)
	if len(n.links[layer]) <= maxLinks {
		return
	}
	cands := make([]item, len(n.links[layer]))
	for i, nb := range n.links[layer] {
		cands[i] = item{nb, dot(n.vec, ix.nodes[nb].vec)}
	}
	sortItems(cands)
	kept := ix.selectNeighbors(cands, maxLinks)
	n.links[layer] = n.links[layer][:0]
	for _, c := range kept {
		n.links[layer] = append(n.links[layer], c.idx)
	}
}

// selectNeighbors applies the paper's heuristic: take candidates (sorted best
// first) that are closer to the base than to any neighbor already chosen, which
// spreads links across directions, then top up with the skipped ones.
func (ix *Index) selectNeighbors(cands []item, m int) []item {
	if len(cands) <= m {
		return cands
	}
	picked := make([]item, 0, m)
	var skipped []item
	for _, c := range cands {
		if len(picked) >= m {
			break
		}
		diverse := true
		for _, p := range picked {
			if dot(ix.nodes[c.idx].vec, ix.nodes[p.idx].vec) >= c.sim {
				diverse = false
				break
			}
		}
		if diverse {
			picked = append(picked, c)
		} else {
			skipped = append(skipped, c)
		}
	}
	for _, c := range skipped {
		if len(picked) >= m {
			break
		}
		picked = append(picked, c)
	}
	return picked
}

// searchLayer is the paper's SEARCH-LAYER. It returns up to ef nodes sorted
// best first. Removed nodes are always traversed; they only appear in the
// result when includeDeleted is set (used while inserting).
func (ix *Index) searchLayer(q []float32, eps []item, ef, layer int, includeDeleted bool) []item {
	visited := make(bitset, (len(ix.nodes)+63)/64)
	frontier := &queue{max: true}
	best := &queue{}
	for _, e := range eps {
		visited.set(e.idx)
		heap.Push(frontier, e)
		if includeDeleted || !ix.nodes[e.idx].deleted {
			heap.Push(best, e)
		}
	}
	for best.Len() > ef {
		heap.Pop(best)
	}
	for frontier.Len() > 0 {
		c := heap.Pop(frontier).(item)
		if best.Len() >= ef && c.sim < best.items[0].sim {
			break
		}
		for _, nb := range ix.nodes[c.idx].links[layer] {
			if visited.has(nb) {
				continue
			}
			visited.set(nb)
			s := dot(q, ix.nodes[nb].vec)
			if best.Len() >= ef && s <= best.items[0].sim {
				continue
			}
			heap.Push(frontier, item{nb, s})
			if includeDeleted || !ix.nodes[nb].deleted {
				heap.Push(best, item{nb, s})
				if best.Len() > ef {
					heap.Pop(best)
				}
			}
		}
	}
	out := make([]item, best.Len())
	for i := len(out) - 1; i >= 0; i-- {
		out[i] = heap.Pop(best).(item)
	}
	return out
}

type item struct {
	idx int32
	sim float32
}

func sortItems(items []item) {
	slices.SortFunc(items, func(a, b item) int {
		if c := cmp.Compare(b.sim, a.sim); c != 0 {
			return c
		}
		return cmp.Compare(a.idx, b.idx)
	})
}

// queue is a heap of items: a max-heap on similarity when max is set, else a
// min-heap. Ties break on node order so traversal is deterministic.
type queue struct {
	items []item
	max   bool
}

func (q *queue) Len() int { return len(q.items) }
func (q *queue) Less(i, j int) bool {
	a, b := q.items[i], q.items[j]
	if a.sim != b.sim {
		return (a.sim > b.sim) == q.max
	}
	return (a.idx < b.idx) == q.max
}
func (q *queue) Swap(i, j int) { q.items[i], q.items[j] = q.items[j], q.items[i] }
func (q *queue) Push(x any)    { q.items = append(q.items, x.(item)) }
func (q *queue) Pop() any {
	last := q.items[len(q.items)-1]
	q.items = q.items[:len(q.items)-1]
	return last
}

type bitset []uint64

func (b bitset) set(i int32)      { b[i>>6] |= 1 << (uint(i) & 63) }
func (b bitset) has(i int32) bool { return b[i>>6]&(1<<(uint(i)&63)) != 0 }

func finite(v []float32) bool {
	for _, x := range v {
		if math.IsNaN(float64(x)) || math.IsInf(float64(x), 0) {
			return false
		}
	}
	return true
}

func dot(a, b []float32) float32 {
	var sum float32
	for i := range a {
		sum += a[i] * b[i]
	}
	return sum
}

// normalized returns v scaled to unit length. A vector that already has unit
// length is returned as is; a zero vector stays zero.
func normalized(v []float32) []float32 {
	var sq float64
	for _, x := range v {
		sq += float64(x) * float64(x)
	}
	if sq == 0 || math.Abs(sq-1) < 1e-4 {
		return v
	}
	inv := float32(1 / math.Sqrt(sq))
	out := make([]float32, len(v))
	for i, x := range v {
		out[i] = x * inv
	}
	return out
}
