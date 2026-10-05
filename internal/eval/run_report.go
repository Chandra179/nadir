package evaluation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// RunReport is the common evaluator/Locust envelope. Results retain each tool's
// native payload; status describes execution, never a quality acceptance gate.
type RunReport struct {
	SchemaVersion int         `json:"schema_version"`
	Tool          string      `json:"tool"`
	Runs          []RunRecord `json:"runs"`
}

type RunRecord struct {
	RunID         string          `json:"run_id"`
	RunType       string          `json:"run_type"`
	Status        string          `json:"status"`
	StartedAt     *string         `json:"started_at"`
	EndedAt       *string         `json:"ended_at"`
	GitRevision   *string         `json:"git_revision"`
	Inputs        map[string]any  `json:"inputs"`
	Provenance    map[string]any  `json:"provenance"`
	Summary       map[string]any  `json:"summary"`
	Results       json.RawMessage `json:"results"`
	Errors        []string        `json:"errors"`
	Artifacts     []any           `json:"artifacts"`
	Normalization *Normalization  `json:"normalization,omitempty"`
}

type Normalization struct {
	OriginalSHA256 string `json:"original_sha256"`
	NormalizedAt   string `json:"normalized_at"`
}

// WriteRunReport replaces a complete envelope atomically, including on failure.
func WriteRunReport(path string, report *RunReport) error {
	if report == nil || report.SchemaVersion != 1 || (report.Tool != "evaluator" && report.Tool != "locust") || len(report.Runs) == 0 {
		return fmt.Errorf("unsupported run report envelope")
	}
	ids := map[string]bool{}
	for _, run := range report.Runs {
		if run.RunID == "" || ids[run.RunID] || run.RunType == "" || run.Inputs == nil || run.Provenance == nil || run.Summary == nil || run.Errors == nil || run.Artifacts == nil {
			return fmt.Errorf("invalid run record")
		}
		ids[run.RunID] = true
		if len(run.Results) > 0 && string(run.Results) != "null" {
			var results map[string]json.RawMessage
			if err := json.Unmarshal(run.Results, &results); err != nil {
				return fmt.Errorf("results must be an object or null")
			}
		}
		switch run.Status {
		case "completed", "failed", "interrupted", "empty":
		default:
			return fmt.Errorf("unsupported run status %q", run.Status)
		}
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal report: %w", err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create report directory: %w", err)
	}
	f, err := os.CreateTemp(dir, ".report-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(append(data, '\n')); err == nil {
		err = f.Close()
	} else {
		_ = f.Close()
	}
	if err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	return os.Rename(f.Name(), path)
}

// NormalizeReport wraps a legacy evaluator report without re-evaluation or
// numeric conversion. Missing historical producer/timing metadata stays null.
func NormalizeReport(source, destination string) error {
	if destination == "" {
		return fmt.Errorf("--normalize-report requires an explicit --report destination")
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(data, &payload); err != nil {
		return err
	}
	var version int
	if err := json.Unmarshal(payload["report_schema_version"], &version); err != nil || version < 1 || version > 2 {
		return fmt.Errorf("source is not a legacy evaluator report")
	}
	var queries []json.RawMessage
	if err := json.Unmarshal(payload["per_query"], &queries); err != nil || queries == nil {
		return fmt.Errorf("source evaluator report requires per_query")
	}
	var aggregate map[string]json.RawMessage
	if err := json.Unmarshal(payload["aggregate"], &aggregate); err != nil || aggregate == nil {
		return fmt.Errorf("source evaluator report requires aggregate")
	}
	var timestamp string
	if err := json.Unmarshal(payload["timestamp"], &timestamp); err != nil {
		return fmt.Errorf("source evaluator report requires timestamp")
	}
	if _, err := time.Parse(time.RFC3339, timestamp); err != nil {
		return fmt.Errorf("source evaluator report has invalid timestamp: %w", err)
	}
	hash := sha256.Sum256(data)
	sha := hex.EncodeToString(hash[:])
	kind := "retrieval"
	summary := map[string]any{"completed_queries": len(queries), "retrieval": payload["aggregate"]}
	status, runErrors := "completed", []string{}
	if len(queries) == 0 {
		status = "empty"
		runErrors = append(runErrors, "original evaluator report contains no queries")
	}
	if generation := payload["generation"]; len(generation) > 0 && string(generation) != "null" {
		kind = "retrieval-and-generation"
		var details struct {
			Aggregate json.RawMessage `json:"aggregate"`
		}
		if err := json.Unmarshal(generation, &details); err != nil {
			return err
		}
		summary["generation"] = details.Aggregate
		var counts struct {
			Failures int `json:"failures"`
		}
		if err := json.Unmarshal(details.Aggregate, &counts); err != nil {
			return err
		}
		if counts.Failures > 0 {
			status = "failed"
			runErrors = append(runErrors, fmt.Sprintf("original generation report records %d operational failures", counts.Failures))
		}
	}
	provenance := payload["provenance"]
	if len(provenance) == 0 || string(provenance) == "null" {
		provenance = json.RawMessage(`{}`)
	}
	return WriteRunReport(destination, &RunReport{SchemaVersion: 1, Tool: "evaluator", Runs: []RunRecord{{
		RunID: "legacy-" + sha[:12], RunType: kind, Status: status,
		Inputs: map[string]any{"dataset": payload["dataset"]}, Provenance: map[string]any{"measurement": provenance},
		Summary: summary, Results: data, Errors: runErrors, Artifacts: []any{},
		Normalization: &Normalization{OriginalSHA256: sha, NormalizedAt: time.Now().UTC().Format(time.RFC3339Nano)},
	}}})
}
