# autoci

`autoci` is an open source CLI for analyzing and optionally validating CI workflows. The first version focuses on Depot CI.

## Install

```bash
go install github.com/autoci-ai/autoci/cmd/autoci@latest
```

## Usage

Analyze the current repository:

```bash
autoci analyze
```

Analyze a specific path:

```bash
autoci analyze --path .
```

Write a Markdown report:

```bash
autoci analyze --report autoci-report.md
```

Generate a runtime profile from recent Depot CI workflow history:

```bash
autoci profile
```

Write a Markdown runtime profile:

```bash
autoci profile --report profile.md
```

Validate with Depot:

```bash
autoci validate --allow-depot-run
```

`autoci validate` refuses to run `depot ci run` unless `--allow-depot-run` is passed. Use `--dry-run` to print the command without executing it.

## Development

```bash
make build
make run
make test
make fmt
make clean
```

## Current Rules

The static analyzer uses simple deterministic heuristics for:

- missing explicit timeouts
- likely unpinned external actions or dependencies
- no obvious caching strategy
- duplicate dependency install steps
- long or complex workflows
- Docker builds that may not use Depot cache effectively
- missing concurrency or cancellation behavior
- workflows that look serial but may be parallelized

The analyzer never runs Depot. Only `autoci validate --allow-depot-run` can execute `depot ci run`.

## Runtime Profiling

`autoci profile` uses Depot workflow execution history to prioritize optimization opportunities with measured evidence. It calls the Depot CLI with JSON output, summarizes recent workflows and jobs, and reports slow jobs, flaky jobs, runtime concentration, repeated failures, and high runtime variance when the sampled history supports those conclusions.

The profile command does not run workflows. It reads history with:

```bash
depot ci workflow list --output json
depot ci workflow show <workflow-id> --output json
```

Useful options:

```bash
autoci profile --limit 100
autoci profile --repo owner/name
autoci profile --report profile.md
```
