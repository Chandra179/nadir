package lexical

import (
	"reflect"
	"testing"
)

func TestTokensAreLowercaseLetterAndDigitRuns(t *testing.T) {
	got := Tokens("Power-rule, POWER rule! x² 404")
	want := []string{"power", "rule", "power", "rule", "x", "404"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Tokens() = %#v, want %#v", got, want)
	}
	if got := Tokens("  ...  "); len(got) != 0 {
		t.Fatalf("punctuation only = %#v", got)
	}
}

func TestQueryTermsDropQuestionWordsButKeepNegationAndNumbers(t *testing.T) {
	got := QueryTerms("Why does our HTTP response not use 404 for an empty search?")
	want := []string{"http", "response", "not", "use", "404", "empty", "search"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("QueryTerms() = %#v, want %#v", got, want)
	}
	if got := QueryTerms("what is it"); len(got) != 0 {
		t.Fatalf("only stopwords = %#v", got)
	}
}

func TestContextualText(t *testing.T) {
	if got := ContextualText("a.md", "Intro", "body"); got != "a.md > Intro\nbody" {
		t.Fatalf("with header = %q", got)
	}
	if got := ContextualText("a.md", "", "body"); got != "a.md\nbody" {
		t.Fatalf("without header = %q", got)
	}
}
