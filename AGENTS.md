# Agent Instructions

## Before pushing changes

Never push until the local CI run passes. Run it first on every branch, even for a docs-only change.

Run the full CI check suite locally via devenv:

```
devenv shell check
```

This runs the same checks as the PR CI workflow (`.github/workflows/ci.yml`):

1. `gofmt` — formatting (read-only check, fails if unformatted)
2. `govet` — static analysis
3. `golangci-lint` — linter
4. `gotest` — tests (no cache: `-count=1`)

Individual checks are also available as devenv scripts: `fmt`, `vet`, `lint`, `test`.

When devenv is unavailable, run the steps from `.github/workflows/ci.yml` directly and keep them identical. A narrower local set hides failures.

If `check` fails, run the corresponding fix script (e.g. `fmt` to auto-format), then re-run `check`.
