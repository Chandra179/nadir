package cache

import "testing"

func TestEmbedQueryUsesConfiguredPrefix(t *testing.T) {
	d := &dependencies{queryPrefix: "search_query: "}
	if got := d.embedQuery("what is x?"); got != "search_query: what is x?" {
		t.Fatalf("embedQuery() = %q, want prefixed query", got)
	}
}

func TestEffectiveVersionChangesAfterInvalidation(t *testing.T) {
	if first, second := effectiveVersion("embed:v1", 0), effectiveVersion("embed:v1", 1); first == second {
		t.Fatalf("cache generations must produce distinct versions: %q", first)
	}
	if got := effectiveVersion("", 3); got != ":g3" {
		t.Fatalf("effectiveVersion() = %q, want generation-bearing version", got)
	}
}
