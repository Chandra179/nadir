# CLAUDE.md

This file provides guidance to Claude Code when working in this repository.

## Agent skills

### Issue tracker

Issues and PRDs for this GitHub repository are published as GitHub issues. See `docs/agents/issue-tracker.md`.

### Triage labels

Use the canonical labels `needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, and `wontfix`. See `docs/agents/triage-labels.md`.

### Domain docs

This is a single-context repository with one root `CONTEXT.md` and system decisions in `docs/adr/`. See `docs/agents/domain.md`.

## Repository guidance

Use [AGENTS.md](AGENTS.md) for the current commands, package boundaries, configuration rules, and local/Compose workflow. See [docs/adr/index.md](docs/adr/index.md) for accepted architecture decisions and [TODO.md](TODO.md) for the open backlog. Keep the public JSON/SSE and Qdrant data contracts compatible when changing the backend.
