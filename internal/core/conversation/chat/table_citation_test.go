package chat

import (
	"context"
	"strings"
	"testing"

	"nadir/internal/core/retrieval/search"
)

func TestLiteralTableCellUsesQueryRowAndSourceDeclaredColumnAlias(t *testing.T) {
	const table = "| Region | temp | Humidity |\n|---|---|---|\n| North | 12.5 | 70 |\n| South | 18 | 60 |"
	for _, text := range []string{table, strings.ReplaceAll(table, "\n", " ")} {
		history := &fakeHistory{}
		chunks := []search.Chunk{
			{Text: text, Header: "Climate > Readings"},
			{Text: "Measurements use degrees Celsius.", Header: "Climate > Temperature (temp)"},
		}
		d := NewDependencies(DependenciesConfig{History: history, Searcher: &fakeSearcher{chunks: chunks}, Generator: &fakeGenerator{tokens: []string{"12.5 ", "[", "2", "]"}}})
		turn := d.StartTurn(context.Background(), Request{Query: "What is the temperature for North?", Generate: true})
		answer, terminal := drain(t, d, turn)
		if answer != "12.5 [1]" || terminal != EventDone {
			t.Fatalf("literal cell cited definition: %q, %v", answer, terminal)
		}
		waitFor(t, func() bool { return len(history.turns()) == 1 })
		if history.turns()[0].Answer != answer || correctCitationAttributions(turn.Query, "12.5 [2]", turn.Citations) != answer {
			t.Fatal("stream, history and evaluator citation correction differ")
		}
	}
}

func TestTableAttributionDoesNotGuessRowColumnOrComputeValues(t *testing.T) {
	table := "| Region | Count | Target |\n|---|---|---|\n| Zone 2 | 42 | 50 |\n| Zone 2.5 | 99 | 42 |"
	citations := []Citation{{Number: 1, Text: table}, {Number: 2, Text: "An overview."}}
	for _, tc := range []struct{ query, answer string }{
		{"What is the count?", "42 [2]"},
		{"What is the value in Zone 2?", "42 [2]"},
		{"What is the count in Zone 2.5?", "42 [2]"},
		{"What are the count and target in Zone 2?", "42 [2]"},
		{"What is the count in Zone 2?", "The count is 42 [2]"},
		{"How much does the count differ from target in Zone 2?", "8 [2]"},
		{"How much does the count differ from target in Zone 2?", "42 [2]"},
	} {
		if got := correctCitationAttributions(tc.query, tc.answer, citations); got != tc.answer {
			t.Fatalf("guessed table support: query=%q answer=%q got=%q", tc.query, tc.answer, got)
		}
	}
	duplicate := append(citations, Citation{Number: 3, Text: table})
	if got := correctCitationAttributions("What is the count in Zone 2?", "42 [2]", duplicate); got != "42 [2]" {
		t.Fatalf("ambiguous table attribution changed: %q", got)
	}
}

func TestMalformedTableFragmentsCannotPanicOrEstablishAttribution(t *testing.T) {
	for _, text := range []string{"|", "| |", "| | |", "|\n|\n|", "| Header |\n|---|\n| value |", "| A | B |\n|--|---|\n| x | 42 |", "| A | B |\n|---|---|\n| x |"} {
		if got := tableValueSource("What is B for x?", "42", []Citation{{Number: 1, Text: text}}); got != 0 {
			t.Fatalf("malformed table established a source: %q", text)
		}
	}
}

func TestRecordedUnitConversionRequiresBothUnitsAndTheLiteralCell(t *testing.T) {
	citations := []Citation{{Number: 1, Text: "| Meters | Feet |\n|---|---|\n| 30 | 98.4 |\n| 40 | 131.2 |"}, {Number: 2, Text: "Feet measure distance."}}
	if got := correctCitationAttributions("30 meters expressed in feet", "98.4 [2]", citations); got != "98.4 [1]" {
		t.Fatalf("recorded conversion cited definition: %q", got)
	}
	for _, query := range []string{"30 seconds expressed in feet", "40 meters expressed in feet", "35 meters expressed in feet"} {
		if got := correctCitationAttributions(query, "98.4 [2]", citations); got != "98.4 [2]" {
			t.Fatalf("units, row or absent conversion guessed: %q for %q", got, query)
		}
	}
}
