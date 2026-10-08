# Chat use-case

Owns sessions and turn lifecycle: edit/prune, retrieval orchestration,
generation supervision, cancellation, event replay, bounded retention, and
history mutation ordering.

Change here for user-visible Chat behavior or lifecycle invariants. Use
`retrieval/` for ranking and `edge/http/chat/` for wire mapping. Verify
with `go test -race ./internal/core/conversation/chat`.

`StartTurn` returns once retrieval and prompt assembly finish. The supervisor
goroutine then dials the answer model, which Ollama answers only after it loads
the model and evaluates the prompt, so the sources and the stream URL reach the
client before the first token. A failure to start the model therefore arrives as
a stream error event (and is persisted as the turn's `GenerateError`), not in
the POST response; a cancel or shutdown during the dial ends the turn without a
provider error. See ADR 0037.

`interface.go` contains the public `Chat` contract; the history persistence
seam is private in `private_interfaces.go`.

Conversation history can persist a selected source section as `Subject`.
Follow-up retrieval carries that section label independently of the optional
rewriter; generation still receives the original question and ranked evidence.
The label resolves a reference, supplies no factual evidence, and does not
exclude contrasting sources. Ambiguous comparisons do not select one subject.
Explicitly named topic changes clear the old selection. Legacy turns can derive
one selection from an explicitly named, cited answer.

Two narrow preflight guards operate on admitted evidence: a conditional event
mentioned only under a competing sibling section declines for the selected
subject; a literal IP-address lookup declines when no valid address is present.
Neither guard claims general answerability. Selected/shared evidence mentioning
the event permits generation; unknown phrasing still relies on generation.
Documented addresses and conceptual/how-to questions are permitted.

For narrow explicit comparisons, the API and evaluator share a deterministic
path: an unqualified recorded speed ordering can answer the named pair;
a difference question can return a complete literal contrast paragraph that
contains all named-option terms; a paired policy-reason question requires both
complete explicitly labeled source paragraphs. This is an excerpt selection policy, not proof
of arbitrary semantic relevance. Conditions remain in the quote. Conflicting
orders, ambiguous paragraphs and unknown forms retain normal generation. The
evaluator records `answer_method` so a literal answer is not reported as a model
call. This path consumes only currently admitted evidence.

Streaming citation correction recognizes complete verbatim assertions, an exact
heading-qualified property, a complete named list whose field matches the
question, a complete explicit subsection label or vertical list, or an exact table value with an
identified row and column. Valid old flattened tables are restored to rows in
the prompt and citation snapshots, preserving empty cells and stored source
identity; malformed fragments and code operators stay literal. Table
column aliases must be declared in admitted headings. Repeated windows from
the same versioned section share a source identity. Different sources,
paraphrases, unanchored short values, literal brackets and omitted conditions
remain unchanged. This is attribution, not a semantic support validator.
Evaluator answers use the same correction; replay and saved history use the
corrected stream. Adjacent citation markers refer to the same complete assertion;
redundant identical markers can remain. An exact copied formula can regain a
recorded scalar inequality suffix only from currently cited complete evidence;
no mathematical restriction is inferred. The prompt explicitly requests all asked alternatives and
preservation of source restrictions, but these instructions are not a proof
that the model obeyed them.
