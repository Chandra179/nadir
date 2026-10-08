package chat

import (
	"context"
	"testing"

	"nadir/internal/core/retrieval/search"
)

func TestLiveStateRequestDeclinesWithoutTreatingExamplesAsObservations(t *testing.T) {
	const expected = "I can search your indexed notes, but I cannot observe the current state of your systems."
	for _, query := range []string{
		"What is my server's memory usage right now?",
		"How many messages are currently waiting in our queue?",
		"Which process on my laptop currently uses the most memory?",
		"What is the latest committed offset of my running connector right now?",
		"What is the current committed LSN of our running production CDC connector?",
		"What exact current driver count does Uber officially report for today?",
		"What exact percentage of today's YouTube uploads uses AV1 in the actual production system?",
	} {
		history := &fakeHistory{}
		generator := &fakeGenerator{tokens: []string{"The example value is the actual value [1]."}}
		d := NewDependencies(DependenciesConfig{History: history, Generator: generator, Searcher: &fakeSearcher{chunks: []search.Chunk{{Text: "Example value: 410"}}}})
		turn := d.StartTurn(context.Background(), Request{Query: query, Generate: true})
		if turn.Streaming || !turn.HasAnswer || turn.Answer != expected || generator.received() != "" || len(turn.Citations) != 0 {
			t.Fatalf("live state reached unsupported generation: query=%q turn=%+v", query, turn)
		}
		waitFor(t, func() bool { return len(history.turns()) == 1 })
		if history.turns()[0].Answer != turn.Answer {
			t.Fatal("decline was not persisted")
		}
	}
}

func TestStaticTechnicalAndSourceQuestionsStillGenerate(t *testing.T) {
	for _, query := range []string{
		"How does live streaming differ from uploaded video?",
		"How many Redis Cluster slots are there?",
		"What do our notes say about the current configuration?",
		"How should I inspect my queue's current depth?",
		"Which example offset do my notes use right now?",
		"What do the design notes say about today's production codec choices?",
		"How should I read our current committed LSN?",
	} {
		generator := &fakeGenerator{tokens: []string{"Source answer [1]."}}
		d := NewDependencies(DependenciesConfig{Generator: generator, Searcher: &fakeSearcher{chunks: []search.Chunk{{Text: "source evidence"}}}})
		turn := d.StartTurn(context.Background(), Request{Query: query, Generate: true})
		if !turn.Streaming {
			t.Fatalf("static question was blocked: %q %+v", query, turn)
		}
		drain(t, d, turn)
	}
}

func TestAddressLookupDeclinesAbsentLiteralButAllowsDocumentedAddress(t *testing.T) {
	const query = "What is the IP address of our deployment primary?"
	for _, tc := range []struct {
		text    string
		decline bool
	}{
		{"Each primary runs on a separate VM. Example: host:{client_ip}.", true},
		{"Primary address: 999.1.2.3", true},
		{"Primary address: 192.0.2.71.", false},
		{"Primary endpoint: http://192.0.2.71:8100/", false},
		{"Primary address: [2001:db8::17]", false},
	} {
		h := &fakeHistory{}
		g := &fakeGenerator{tokens: []string{"Source answer [1]."}}
		d := NewDependencies(DependenciesConfig{History: h, Generator: g, Searcher: &fakeSearcher{chunks: []search.Chunk{{Text: tc.text}}}})
		turn := d.StartTurn(context.Background(), Request{Query: query, Generate: true})
		if tc.decline {
			if turn.Streaming || !turn.HasAnswer || turn.Answer != "The retrieved notes do not specify that IP address." || g.received() != "" {
				t.Fatalf("missing address became an answer: %+v", turn)
			}
		} else {
			if !turn.Streaming {
				t.Fatalf("documented address was blocked: %+v", turn)
			}
			drain(t, d, turn)
		}
		waitFor(t, func() bool { return len(h.turns()) == 1 })
	}
	for _, question := range []string{"What is an IP address?", "How can I determine the IP address of our primary?", "What is the purpose of IP address lookup?"} {
		if missingLiteralAddress(question, nil) != "" {
			t.Fatalf("conceptual/how-to question blocked: %q", question)
		}
	}
}
