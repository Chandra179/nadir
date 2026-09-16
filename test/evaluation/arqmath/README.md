# ARQMath public evaluation pack

This directory contains the reproducible ARQMath Task 1 query-selection
metadata. The checked-in pack selects 40 topics from each ARQMath-1 (2020),
ARQMath-2 (2021), and ARQMath-3 (2022) edition: 120 public math questions in
total. It is a candidate for release-gate review, not a release gate.

ARQMath and Math Stack Exchange data have non-commercial and attribution
requirements. Read the official [ARQMath resources](https://www.cs.rit.edu/~dprl/ARQMath/arqmath-resources.html),
[ARQMath guidelines](https://www.cs.rit.edu/~dprl/ARQMath-backup/ARQMath-Guidlines-v5.3.pdf),
and [Stack Exchange licensing terms](https://opendata.stackexchange.com/help/licensing)
before downloading or redistributing the data.

## Build the candidate pack

Use an ignored working directory. The importer downloads and verifies the six
small topic/qrels artifacts using the hashes in `SOURCES.json`:

```bash
python3 scripts/import_arqmath.py build \
  --source-dir var/evaluation/arqmath/source \
  --output-dir var/evaluation/arqmath/pack \
  --per-edition 40
```

The full `Posts.V1.3.zip` collection is required for a representative corpus
and is intentionally not checked into Git. Supply a trusted, independently
obtained SHA-256 before building a reviewable corpus:

```bash
python3 scripts/import_arqmath.py build \
  --source-dir var/evaluation/arqmath/source \
  --output-dir var/evaluation/arqmath/pack \
  --posts var/evaluation/arqmath/source/Posts.V1.3.zip \
  --posts-sha256 <trusted-posts-sha256> \
  --per-edition 40
```

When the archive has not been downloaded yet, provide the same trusted hash
with `--posts-sha256` and the importer downloads the official archive from its
default URL. Use `--posts-url` to pin an approved mirror explicitly.

The generated pack remains `release_gate: false` until two real, independent
math-capable reviewers label every query and its fixed candidate pool.

Each build also writes `arqmath-review-report.json`. It records the source
hashes and 120-query distribution; its Retrieval evaluation is explicitly
`not_run` until the representative corpus is indexed and the human review
pack is complete.

## Merge genuine review evidence

Each reviewer submits a JSON file with a stable identity, role, verification
reference, and one judgment for every query:

```json
{
  "reviewer": {
    "id": "reviewer-a",
    "role": "math domain expert",
    "human": true,
    "independent": true,
    "verification_ref": "reviews/reviewer-a-attestation.md"
  },
  "judgments": [
    {
      "query_id": "arqmath-2020-A.1",
      "relevant": [{"document_id": "123", "grade": 3}],
      "distractors": [{"document_id": "456", "grade": 0}]
    }
  ]
}
```

Every fixed candidate document must occur exactly once across `relevant` and
`distractors`; relevant labels use a positive grade and distractors use grade
zero. The example is abbreviated—real review files must include the complete
candidate pool.

The adjudication file must provide an expected answer, required claims,
faithfulness label, and canonical relevant/distractor labels for every query.
The privacy-review file must identify an approved review and its evidence
hash. Then merge the two records:

```bash
python3 scripts/import_arqmath.py merge \
  --review-pack var/evaluation/arqmath/pack/arqmath-review-pack.json \
  --reviewer reviews/reviewer-a.json \
  --reviewer reviews/reviewer-b.json \
  --adjudication reviews/adjudication.json \
  --privacy-review reviews/privacy.json \
  --output var/evaluation/arqmath/arqmath-golden.json
go run ./cmd/evaluator --validate-only --require-release-gate \
  --golden var/evaluation/arqmath/arqmath-golden.json
```

Do not fabricate reviewer identities, consent approvals, or labels. The
committed synthetic fixture in `../golden.json` remains the regression test.
