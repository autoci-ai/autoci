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

The initial analyzer uses simple deterministic heuristics for:

- missing explicit timeouts
- likely unpinned external actions or dependencies
- no obvious caching strategy
- duplicate dependency install steps
- long or complex workflows
- Docker builds that may not use Depot cache effectively
- missing concurrency or cancellation behavior
- workflows that look serial but may be parallelized

The analyzer never runs Depot. Only `autoci validate --allow-depot-run` can execute `depot ci run`.
