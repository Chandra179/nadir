package evaluation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// FileFingerprint records content of configured local source files. It does
// not attest that the remote index contains those exact versions.
type FileFingerprint struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Bytes  int    `json:"bytes"`
}

type CorpusFingerprint struct {
	Files         []FileFingerprint `json:"files"`
	ContentSHA256 string            `json:"content_sha256"`
	Scope         string            `json:"scope"`
}

// ModelFingerprint is observed serving metadata, not a claim of judge quality.
type ModelFingerprint struct {
	Role          string `json:"role"`
	Model         string `json:"model"`
	Digest        string `json:"digest,omitempty"`
	Family        string `json:"family,omitempty"`
	Parameters    uint64 `json:"parameters,omitempty"`
	ParameterSize string `json:"parameter_size,omitempty"`
	Quantization  string `json:"quantization,omitempty"`
	Status        string `json:"status"`
	Error         string `json:"error,omitempty"`
}

type ReportProvenance struct {
	GoldenSHA256          string             `json:"golden_sha256"`
	DeclaredCorpusSHA256  string             `json:"declared_corpus_sha256,omitempty"`
	FixtureCorpus         *CorpusFingerprint `json:"fixture_corpus,omitempty"`
	ConfiguredSources     CorpusFingerprint  `json:"configured_sources"`
	CorpusManifestPath    string             `json:"corpus_manifest_path,omitempty"`
	CorpusManifestSHA256  string             `json:"corpus_manifest_sha256,omitempty"`
	EffectiveConfig       any                `json:"effective_config"`
	EffectiveConfigSHA256 string             `json:"effective_config_sha256"`
	Models                []ModelFingerprint `json:"models"`
}

func HashJSON(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:]), nil
}

// SameModelName catches implicit :latest tags before serving metadata can
// reveal aliases with an identical digest.
func SameModelName(left, right string) bool {
	normalize := func(value string) string {
		value = strings.ToLower(strings.TrimSpace(value))
		if !strings.Contains(value, ":") {
			value += ":latest"
		}
		return value
	}
	return normalize(left) == normalize(right)
}

func HashFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:]), nil
}

// FingerprintCorpus uses sorted, slash-normalized relative paths plus exact
// bytes, separated by NUL. This agrees with the candidate generator's digest.
func FingerprintCorpus(paths []string) (CorpusFingerprint, error) {
	paths = append([]string(nil), paths...)
	sort.Strings(paths)
	out := CorpusFingerprint{Scope: "configured local source bytes; indexed-version equality is not verified", Files: make([]FileFingerprint, 0, len(paths))}
	digest := sha256.New()
	seen := map[string]bool{}
	for _, path := range paths {
		path = filepath.ToSlash(filepath.Clean(path))
		if seen[path] {
			continue
		}
		seen[path] = true
		data, err := os.ReadFile(path)
		if err != nil {
			return out, fmt.Errorf("fingerprint %s: %w", path, err)
		}
		hash := sha256.Sum256(data)
		out.Files = append(out.Files, FileFingerprint{Path: path, SHA256: hex.EncodeToString(hash[:]), Bytes: len(data)})
		digest.Write([]byte(path))
		digest.Write([]byte{0})
		digest.Write(data)
		digest.Write([]byte{0})
	}
	out.ContentSHA256 = hex.EncodeToString(digest.Sum(nil))
	return out, nil
}
