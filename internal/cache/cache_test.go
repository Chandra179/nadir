package cache

import "testing"

func TestEmbedQueryUsesConfiguredPrefix(t *testing.T) {
	d := &dependencies{queryPrefix: "search_query: "}
	if got := d.embedQuery("what is x?"); got != "search_query: what is x?" {
		t.Fatalf("embedQuery() = %q, want prefixed query", got)
	}
}
