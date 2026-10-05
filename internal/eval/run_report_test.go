package evaluation

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestNormalizeReportPreservesNumbersAndUnknownHistory(t *testing.T) {
	source := []byte(`{"report_schema_version":2,"timestamp":"2026-10-03T06:44:39Z","dataset":"test","per_query":[],"aggregate":{"huge":9007199254740993,"precise":0.1234567890123456789},"provenance":{"golden_sha256":"fixture"}}`)
	dir := t.TempDir()
	input, output := filepath.Join(dir, "legacy.json"), filepath.Join(dir, "report.json")
	if err := os.WriteFile(input, source, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := NormalizeReport(input, output); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var envelope RunReport
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatal(err)
	}
	run := envelope.Runs[0]
	if envelope.Tool != "evaluator" || run.StartedAt != nil || run.EndedAt != nil || run.GitRevision != nil {
		t.Fatalf("invented historical metadata: %+v", run)
	}
	decode := func(data []byte) any {
		d := json.NewDecoder(bytes.NewReader(data))
		d.UseNumber()
		var value any
		if err := d.Decode(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	if !reflect.DeepEqual(decode(source), decode(run.Results)) {
		t.Fatal("original payload changed")
	}
	hash := sha256.Sum256(source)
	if run.Normalization.OriginalSHA256 != hex.EncodeToString(hash[:]) {
		t.Fatal("wrong source hash")
	}
	if err := NormalizeReport(output, output); err == nil {
		t.Fatal("accepted a double-wrapped report")
	}
}

func TestNormalizeReportRejectsUnrelatedResults(t *testing.T) {
	for _, source := range []string{`{"measured_at":"today","checks":[]}`, `{"report_schema_version":2,"per_query":[]}`, `null`} {
		path := filepath.Join(t.TempDir(), "source.json")
		if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := NormalizeReport(path, path+".out"); err == nil {
			t.Fatalf("accepted %s", source)
		}
	}
}

func TestNormalizeReportRecordsOperationalGenerationFailures(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source.json")
	source := `{"report_schema_version":2,"timestamp":"2026-10-03T00:00:00Z","per_query":[{"id":"q"}],"aggregate":{},"generation":{"aggregate":{"failures":1}}}`
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := NormalizeReport(path, path+".out"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path + ".out")
	if err != nil {
		t.Fatal(err)
	}
	var report RunReport
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	if report.Runs[0].Status != "failed" || len(report.Runs[0].Errors) != 1 {
		t.Fatalf("operational failure was hidden: %+v", report)
	}
}

func TestRunReportMatchesSharedContract(t *testing.T) {
	data, err := os.ReadFile("../../test/run-report-contract.json")
	if err != nil {
		t.Fatal(err)
	}
	var contract struct {
		SchemaVersion  int      `json:"schema_version"`
		Tools          []string `json:"tools"`
		Statuses       []string `json:"statuses"`
		EnvelopeFields []string `json:"required_envelope_fields"`
		RunFields      []string `json:"required_run_fields"`
	}
	if err := json.Unmarshal(data, &contract); err != nil {
		t.Fatal(err)
	}
	for _, tool := range contract.Tools {
		for _, status := range contract.Statuses {
			report := &RunReport{SchemaVersion: contract.SchemaVersion, Tool: tool, Runs: []RunRecord{{
				RunID: "run", RunType: "retrieval", Status: status, Inputs: map[string]any{},
				Provenance: map[string]any{}, Summary: map[string]any{}, Errors: []string{}, Artifacts: []any{},
			}}}
			path := filepath.Join(t.TempDir(), "report.json")
			if err := WriteRunReport(path, report); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var envelope map[string]json.RawMessage
			if err := json.Unmarshal(data, &envelope); err != nil {
				t.Fatal(err)
			}
			for _, field := range contract.EnvelopeFields {
				if _, ok := envelope[field]; !ok {
					t.Fatalf("missing envelope field %s", field)
				}
			}
			var runs []map[string]json.RawMessage
			if err := json.Unmarshal(envelope["runs"], &runs); err != nil {
				t.Fatal(err)
			}
			for _, field := range contract.RunFields {
				if _, ok := runs[0][field]; !ok {
					t.Fatalf("missing run field %s", field)
				}
			}
		}
	}
}

func TestWriteRunReportRejectsInvalidRecords(t *testing.T) {
	valid := RunRecord{RunID: "run", RunType: "retrieval", Status: "completed", Inputs: map[string]any{},
		Provenance: map[string]any{}, Summary: map[string]any{}, Errors: []string{}, Artifacts: []any{}}
	for _, mutate := range []func(*RunReport){
		func(r *RunReport) { r.SchemaVersion = 2 },
		func(r *RunReport) { r.Tool = "temporary" },
		func(r *RunReport) { r.Runs = append(r.Runs, valid) },
		func(r *RunReport) { r.Runs[0].Status = "passed" },
		func(r *RunReport) { r.Runs[0].Inputs = nil },
		func(r *RunReport) { r.Runs[0].Errors = nil },
		func(r *RunReport) { r.Runs[0].Results = json.RawMessage(`[]`) },
	} {
		report := &RunReport{SchemaVersion: 1, Tool: "evaluator", Runs: []RunRecord{valid}}
		mutate(report)
		if err := WriteRunReport(filepath.Join(t.TempDir(), "report.json"), report); err == nil {
			t.Fatalf("accepted invalid record: %+v", report)
		}
	}
}
