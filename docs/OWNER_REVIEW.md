# Owner review

Personal usefulness needs questions you actually ask and your judgment of the
answers. The engineering acceptance in [P1_EVIDENCE.md](P1_EVIDENCE.md) uses
simulated questions and cannot replace this review.

The ignored local file `.local/local-v1/owner-review/questions.json` contains
ten proposed questions about the recovered business note. They are agent
drafts, saved before any run, with the original note's content hash. Replace
them with your common questions or explicitly confirm the ones you use. Keep
the author and confirmation metadata accurate. Add each other source file and
its SHA-256 to `metadata.corpus` if your questions use additional notes.

For each question, save the facts you expect or the limitation the app should
state before testing. A follow-up can set `session_from` to an earlier case ID.
The existing runner verifies the saved corpus hashes, records actual answers
and citations, and removes only sessions it created unless `--keep-sessions`
is requested. Empty `relevant` lists mean no expected-source annotation;
citation mapping checks alone never establish correctness.

Start the copied-config local app using [LOCAL_V1.md](LOCAL_V1.md), then run
from the repository root:

```bash
python3 scripts/run_daily_use.py \
  --fixture .local/local-v1/owner-review/questions.json \
  --report .local/local-v1/owner-review/answers.json
```

The output must be a new path. Keep owner questions, answers, and note excerpts
under ignored `.local/`; do not commit them without a separate decision to
publish that content. Save your review beside the answers:

| Case ID | Useful? | Facts supported by cited section? | Requested facts complete? | Limitation clear? | Reason / reproduction |
|---|---|---|---|---|---|
| Each of the ten saved cases | Yes / No | Yes / No | Yes / No / N/A | Yes / No / N/A | Exact wrong claim, missing fact or misleading citation |

A reproduced material failure needs a focused regression and a fix in its
responsible layer. Missing information needs an authoritative source or a clear
decline. Unknown wording should be assessed from the actual answer, not assumed
safe because the narrow English guards passed their tests. Owner acceptance
remains pending until this review is completed.

Independent judge calibration is a separate task. Its blind packet has 39
cases in [the manifest](../test/evaluation/judge-calibration/20260930-phi4-mini/manifest.json).
Use two independent human reviewers; each reads the question, answer and
admitted evidence before assigning scores. The automatic judge's scores are
excluded from the packets. Create private working copies, preserving any
existing reviews:

```bash
mkdir -p .local/judge-calibration/20260930-phi4-mini
cp -n test/evaluation/judge-calibration/20260930-phi4-mini/reviewer-a.json \
  .local/judge-calibration/20260930-phi4-mini/reviewer-a.json
cp -n test/evaluation/judge-calibration/20260930-phi4-mini/reviewer-b.json \
  .local/judge-calibration/20260930-phi4-mini/reviewer-b.json
```

Each reviewer fills only their `reviewer` metadata, every required
`human_scores` value in [0, 1], and a `rationale` for every case. Preserve the
questions, answers, context and rubric. IDs must differ; `human` and
`independent` must truthfully be `true`. Supply a dated review evidence reference
in `verification_ref` and an ISO timestamp with timezone in `reviewed_at`.
An agent review cannot supply these attestations.

After both reviews are complete, from the repository root:

```bash
python3 scripts/calibrate_evaluation_judge.py score \
  --manifest test/evaluation/judge-calibration/20260930-phi4-mini/manifest.json \
  --reviews .local/judge-calibration/20260930-phi4-mini/reviewer-a.json \
    .local/judge-calibration/20260930-phi4-mini/reviewer-b.json \
  --output .local/judge-calibration/20260930-phi4-mini/agreement.json
```

The script checks frozen source hashes and packet content. It requires at least
two reviewers and 20 cases for a result within its declared thresholds, with
mean absolute error at most 0.15 and p95 error at most 0.35, including reviewer
agreement. A failed or incomplete calibration remains pending; scoring does
not certify arbitrary future answers or modified models. The packet calibrates
its September 30 snapshot, not the later P1 implementation.
