package contractcheck

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestRepositoryContractMirrorIsAligned(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	if err := Check(filepath.Join(root, "contracts/http/openapi.yaml"), filepath.Join(root, "web/dashboard/src/lib/api-contract.ts")); err != nil {
		t.Fatal(err)
	}
}
