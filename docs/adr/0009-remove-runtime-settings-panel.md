# 0009 — The runtime settings panel is removed; config is static per process

- **Status:** Accepted
- **Date:** 2026-09-05
- **Deciders:** Chandra, ZCode session

## Context

The dashboard had a settings panel rendering the env-override surface. To do
so it maintained a hardcoded `envOverridden` mirror inside
`internal/api/settings.go`, duplicating the `applyEnv()` logic in
`config/config.go`. The two drifted immediately (the panel already showed
keys that no longer existed), and every config addition required touching
both. The panel also implied runtime mutability that the server doesn't
have — every value is read once at startup.

An intermediate fix (recording overrides in `cfg.Overridden` at parse time)
kept the duplication alive with a nicer data flow; it was discarded along
with the panel.

## Decision

Remove the settings UI and its handler entirely — no backward-compatible
endpoint, no read-only replacement. Configuration is `config/config.yaml`
plus env overrides, applied once at startup; the startup log is the
authoritative record of what took effect. The metrics middleware (unused
counters) was removed in the same cleanup sweep.

## Consequences

- Adding a config key touches exactly one place (`config.go`).
- Toggling a feature means editing the file and restarting the process —
  acceptable for a self-hosted single-user deployment.
- The domain-packages-must-not-import-api rule no longer has a settings
  counterexample to argue about.
