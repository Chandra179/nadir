# Documentation

Start with the [repository README](../README.md) for setup and commands. This
index maps each topic to its maintained home.

| Topic | Read here |
|---|---|
| Product behavior, architecture and algorithms | [Overview](overview.md) |
| Current priorities | [TODO](../TODO.md) |
| Domain language and relationships | [CONTEXT](../CONTEXT.md) |
| Backend ownership and package map | [Backend README](../internal/README.md) |
| Commands, dependency boundaries and agent rules | [AGENTS](../AGENTS.md) |
| Configuration values and overrides | [Configuration](../internal/bootstrap/configuration/config.yaml), [agent rules](../AGENTS.md) |
| Go conventions | [Style](style.md), [errors](errors.md), [logging](logging.md) |
| Architecture decisions and their history | [ADR index](adr/index.md) |
| Evaluation commands and fixture definitions | [Evaluation guide](../test/evaluation/README.md) |
| API performance workloads and reports | [Locust benchmarks](../benchmark/README.md) |
| Generated retrieval measurements and archive | [Report catalog](../test/evaluation/reports/README.md) |
| Podman deployment | [Compose guide](../deploy/compose/README.md) |
| Optional model/conversion services | [Reranker](../sidecars/reranker/README.md), [document converter](../sidecars/document-converter/README.md) |
| Agent issue workflow | [Domain](agents/domain.md), [issue tracker](agents/issue-tracker.md), [triage labels](agents/triage-labels.md) |

## Where documentation belongs

Project-wide guides live directly in `docs/` and use lowercase kebab-case names.
Directory entry points use `README.md`. Root `README.md`, `AGENTS.md`,
`CLAUDE.md`, `CONTEXT.md` and `TODO.md` retain their conventional names.
ADRs use numbered `NNNN-kebab-case.md` names and keep their decision history.

Package READMEs live beside the implementation and explain ownership, change
locations and relevant checks. Use the backend map to find them. Agent workflow
notes live in `docs/agents/`; evaluation fixtures and immutable raw evidence live
under `test/evaluation/`.

Update the maintained home of a fact, then link to it elsewhere. Keep measured
results and their limits in the report catalog and dated evidence; the overview
introduces the product. Historical ADR paths and raw reports describe their
recorded snapshot and are not current setup instructions.
