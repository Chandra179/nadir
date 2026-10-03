package qdrantutil

import (
	"fmt"

	qdrant "github.com/qdrant/go-client/qdrant"
)

// ValidateDenseCollection checks the part of a Qdrant collection schema that
// callers rely on for dense-vector writes. Existing collections are not
// automatically migrated: a mismatch is reported at startup instead of
// surfacing later as an opaque search or upsert error.
func ValidateDenseCollection(name string, info *qdrant.CollectionInfo, dimensions int) error {
	collectionParams := info.GetConfig().GetParams()
	if collectionParams == nil {
		return fmt.Errorf("qdrant collection %q has no vector configuration", name)
	}
	params := collectionParams.GetVectorsConfig().GetParams()
	if params == nil {
		return fmt.Errorf("qdrant collection %q has no dense vector configuration", name)
	}
	if params.GetSize() != uint64(dimensions) {
		return fmt.Errorf("qdrant collection %q vector dimensions=%d, want %d", name, params.GetSize(), dimensions)
	}
	if params.GetDistance() != qdrant.Distance_Cosine {
		return fmt.Errorf("qdrant collection %q distance=%s, want cosine", name, params.GetDistance())
	}
	return nil
}

// HasSparseVector reports whether a collection declares the named sparse
// vector configuration.
func HasSparseVector(info *qdrant.CollectionInfo, name string) bool {
	sparse := info.GetConfig().GetParams().GetSparseVectorsConfig()
	_, ok := sparse.GetMap()[name]
	return ok
}
