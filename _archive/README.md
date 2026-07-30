# Archive — the Python implementation

`cu` is a Go program now. This directory holds the Python package it replaced,
kept for reference until the Go port has been exercised against real AWS, Azure
and Password Pusher credentials.

Nothing here is built, tested or shipped by the repo's Taskfile, CI workflows or
Dockerfile. It is inert.

```
cloudutil/     the typer CLI, one subpackage per command group
tests/         the pytest suite the Go tests replaced
pyproject.toml, uv.lock
publish.yml    the PyPI release workflow, moved out of .github/workflows so it
               stops firing on release now that goreleaser publishes the binary
```

## Running it

Fixtures still live at the repo's `tests/assets`, shared with the Go suite, so
the test paths reach back out of this directory.

```sh
cd _archive
uv sync
uv run pytest tests -q
uv run cu --help
```

## Why it is still here

The Go port has no live coverage of the cloud-facing commands — `internal/awsx`
has no test file at all. Everything deterministic (the diff engine, rendering,
config parsing) is covered by 108 Go tests, and the Kubernetes commands have
been run against a real cluster, but `aws login`, `ssm-parameters`, `secrets`,
`decode-message`, `az secrets` and `pwpush` have never executed against a real
endpoint.

Once they have, this directory can go. `git rm -r _archive` in its own commit,
so a single `git revert` brings it back.
