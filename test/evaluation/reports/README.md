# Generated evaluator results

The active catalog contains **four Go evaluator reports**, measured October 2
and October 3, 2026. They measure retrieval over the former sample corpus. They
are historical records; the current reading index and its uploaded documents
have not been evaluated by these runs. The sample corpus has been removed.

| Measurement | Golden queries | Broader queries |
|---|---|---|
| October 2 control | [Report](2026-10-02/quality-comparison/control/retrieval-golden.json) | [Report](2026-10-02/quality-comparison/control/retrieval-broader.json) |
| October 3 candidate | [Report](2026-10-03/local-acceptance/retrieval-golden.json) | [Report](2026-10-03/local-acceptance/retrieval-broader.json) |

The October 2 control was not rerun October 3. The reports preserve their
original corpus/config/model provenance and complete rankings. Their input
hashes match [golden.json](../golden.json) and
[representative.json](../representative.json); the latter also uses
[representative-corpus.json](../representative-corpus.json).

## Shared result format

The Go evaluator and [Locust suite](../../../benchmark/README.md) emit the same
versioned JSON envelope. The [contract](../../run-report-contract.json) defines
its required fields and permitted tools/statuses; focused tests enforce it.

- `schema_version`: `1`; `tool`: `evaluator` or `locust`; `runs`: run records.
- Each run has `run_id`, `run_type`, `status`, `started_at`, `ended_at`,
  `git_revision`, `inputs`, `provenance`, `summary`, `results`, `errors`, and
  `artifacts`. Inputs, provenance and summary are objects; results is an object
  or null; errors is an array of strings. Unknown timestamps/revisions are null.
- Status is `completed`, `failed`, `interrupted`, or `empty`. It describes
  execution. A completed evaluation is not a quality acceptance decision.
- `results` contains the complete native evaluator report or Locust's serialized
  request statistics and exceptions. Summary metrics are specific to each tool.
- Locust artifact records have relative paths, kind, SHA-256 and byte size.
  Each UI run retains its own native CSV/HTML snapshots under `runs/<run_id>/`.

An evaluator invocation records one run. Locust UI starts append runs. New
outputs default to ignored `.local/evaluation/<timestamp>-<run-id>/report.json`
and `.local/benchmark/<timestamp>-<run-id>/report.json`. Explicit output paths
remain supported. Available partial results and operational errors are saved
on failure; an unwritable output location is reported through stderr/nonzero exit.

## Historical normalization and identities

The four reports were wrapped by the evaluator's offline `--normalize-report`
mode on October 5. Their original payloads live in `runs[0].results`, including
the original measurement timestamp, values and provenance. Normalization does
not run retrieval or models. Missing historical start/end times and producer
Git revisions remain null; `normalization` records the original file SHA-256
and wrapping time separately.

The [manifest](manifest.json) maps original identities to active paths and
records both original and normalized hashes. Historical paths inside payloads
resolve through `artifacts[].original_path` to `artifacts[].path`. Archived
entries have a null path. Original report bytes remain recoverable from backups
and Git history.

## Archived experiments and restoration

On October 5, **27 additional reports** were removed from the active catalog:
saved-answer runner output, manual reviews, workflow/migration records, and
validation notes. The earlier cleanup had archived 39 artifacts. The manifest
now indexes 66 archived artifacts and four active reports, plus the three
unchanged dated input fixtures. Input fixtures are not measurement results.

Before this change, 115 affected source, documentation and data files were
backed up and SHA-256 verified in ignored
`.local/report-archive/2026-10-05-tool-generated-64568657/before-change.tar.gz`.
Its `inventory.json` and the manifest's `tool_generated_cleanup` section record
hashes and restoration identities. Extract a last active path from that backup:

```bash
mkdir -p /tmp/nadir-report-restore
tar -xzf .local/report-archive/2026-10-05-tool-generated-64568657/before-change.tar.gz \
  -C /tmp/nadir-report-restore test/evaluation/reports/2026-10-03/local-acceptance/answers-daily.json
```

Local backups are not distributed with Git. The preceding committed files also
remain at revision `e4388bb`. Older archive and calibration retirement records
remain in the manifest. ADRs preserve dated design decisions and archived
interpretations; the active catalog makes no app acceptance claim from them.

## Retention policy

1. Generate results through `cmd/evaluator` or `python -m benchmark`. Keep run
   outputs local by default because inputs/results can contain source excerpts,
   query text and filesystem paths.
2. Commit selected generated reports with their exact input hashes and update
   the catalog/manifest. Preserve generated values; use the maintained offline
   wrapping mode for supported legacy evaluator formats.
3. Keep manual usefulness notes and calibration reviewer inputs separate from
   generated result artifacts. Reports from retired runners or temporary code
   do not enter this catalog.
4. Archive removed files after checking dependencies and backup hashes. Treat
   retained historical records as measurements of their original corpus/config.

The fixture-schema and report-contract tests run in CI. Reporting verification
uses local stubs and fixtures; it does not rerun historical model measurements.
