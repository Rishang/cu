# AGENTS.md

Instructions for agents working in this repository. Read this before writing code.

## Project overview

`cu` (module `github.com/Rishang/cloudutil`) is a single static Go binary that
wraps the cloud and Kubernetes chores that otherwise turn into one-off shell
scripts: AWS console login and SSM, Azure Key Vault, HashiCorp Vault and
Infisical secrets, Kubernetes secrets/ConfigMaps/logs, semantic config diffing,
Password Pusher, and a Taskfile passthrough.

### Goals, in priority order

1. **One binary, nothing to install.** `fzf` is compiled in as a library, not
   shelled out to. A user with the binary has the whole tool.
2. **Interactive by default, scriptable always.** Every browse command is
   `list → fzf → print`. Status text goes to stderr so stdout stays a clean
   pipe into `jq`.
3. **Inherit the user's existing setup.** Credentials, contexts, profiles and
   auth plugins come from the tools people already configured (`kubectl`,
   `~/.aws`, `az login`) rather than being re-modelled here.
4. **Least code that works.** No speculative abstraction, no layer with one
   caller, no dependency for what a few lines of stdlib do.

### Layout

| Path | Contents |
|---|---|
| `main.go` | Version stamping, signal handling, exit codes |
| `internal/cli/` | One file per command group, wired in `root.go` |
| `internal/ui/` | Output: styles, tables, JSON, stdout/stderr split |
| `internal/pick/` | fzf-as-a-library selection |
| `internal/kube/` | kubectl invocation and parsing |
| `internal/awsx/` | AWS SDK helpers |
| `internal/diff/` | Semantic diff engine and its config |
| `tests/assets/` | Fixtures shared by the Go tests |

## Build, test, lint

Use the Taskfile — it stamps version metadata the same way the release build
does, so don't hand-roll `go build`:

```bash
task build        # -> dist/cu, with version/commit/date ldflags
task test         # go test -race ./...
task lint         # gofmt -l -w ., then go vet ./...
task              # lists the rest (install, tidy, release)
```

`task lint` and `task test` must both pass before you call work done. Report
failures with their output rather than describing them.

## Code standards

### Comments explain why, never what

A comment earns its place by recording a decision, a constraint, or a trap — not
by restating the code beneath it.

```go
// Buffered to len(labels): fzf can never emit more lines than it was given,
// so it cannot block on Output while we wait for Run to return.
out := make(chan string, len(labels))
```

Bad: `// loop over the secrets`. Delete it. If a block needs a what-comment, the
names are wrong.

Every exported identifier gets a doc comment, starting with its name.

### Output discipline

- `ui.Out` (stdout) is **data**: JSON payloads, generated URLs. `ui.PrintJSON`.
- `ui.Err` (stderr) is **status**: `ui.Info` `[*]`, `ui.Warn` `[!]`,
  `ui.Error` `[ERROR]`. Never print status to stdout.
- Colour goes through `ui.Style` (`ui.Green`, `ui.Cyan`, …), which uses ANSI
  30–37 so it follows the user's theme, and respects `NO_COLOR` / `TERM=dumb`
  via `ui.ColorEnabled()`. Anything that conveys information by colour alone
  needs a plain-text fallback when colour is off.

### Errors

- Return errors; never `os.Exit` or `log.Fatal` outside `main.go`.
- Wrap with `%w` when the cause matters, and include what was attempted:
  `fmt.Errorf("kubectl %s failed: %s", …)`.
- A specific exit code is `exitWith(n)` / `cli.ExitCodeError`, which `main`
  translates. The command has already printed anything the user needs.
- Lowercase messages, no trailing punctuation.
- "Not visible to these credentials" is usually **not** an error: skip that
  subtree and carry on (see `denied()` in `internal/cli/secrets.go`).

### Command structure

- One `newXxxCommand()` constructor per command, `RunE` not `Run`, `Args`
  always set explicitly.
- Take `cmd.Context()` and thread it through every network call so Ctrl-C works.
- Share the `list → fzf → print` flow via `pickFrom` / `pickStrings` /
  `pickAndPrint` in `internal/cli/root.go`. Don't reimplement it.
- Flag help is a sentence ending in a period. Mention the env var in brackets:
  `"Profile from … [$VAULT_PROFILE]."`

### Dependencies

Climb this ladder before adding anything to `go.mod`:

1. Does the code need to exist at all?
2. Is it already in this repo? (`internal/ui`, `internal/pick`, `configHome()`,
   `decodeSecretValue`, `splitPrefix`, `apiRequest` …)
3. Does the stdlib do it? `path.Join` over slash arithmetic, `cmp.Or` over
   an if/else default, `slices`/`maps` over hand-rolled loops.
4. Is it already an indirect dependency? (`golang.org/x/sync` was.)
5. Only then a new module — and say why in the commit.

REST APIs use `net/http` and a small local struct, not a vendor SDK, unless the
SDK is already here (AWS, Azure).

### Concurrency

Fan out with `golang.org/x/sync/errgroup` plus `SetLimit(listConcurrency)`.
Never recurse into a bounded errgroup — a parent blocking in `Go()` while
holding a slot deadlocks against its own children. Go breadth-first with a
barrier per level instead. Keep results order-stable by writing into an indexed
slice, not appending from goroutines.

### Config files

`configHome()` (`~/.config/cu`) is the only location. Anything holding a
credential is written `0600` with an explicit `os.Chmod` afterwards, since
`WriteFile`'s mode only applies on creation.

### Tests

- Standard `testing` only. No frameworks, no mocking libraries, no fixtures
  beyond `t.TempDir()`.
- One runnable check per piece of non-trivial logic (a branch, a parser, a
  credential path), testing the contract rather than the implementation; trivial
  one-liners need none. Table-driven where there are cases, `t.Run` subtests
  named for the behaviour it asserts.
- **Prefer the real thing over a stub.** `kubectl config` needs no cluster, so
  test against real `kubectl` behind an `exec.LookPath` skip. Use `httptest`
  for HTTP APIs and assert on the request the client actually made.
- A failure message says what broke and why it matters, not just `got != want`.
- `-race` is not optional; guard shared state in fake HTTP handlers, which run
  on several goroutines when the client fans out.
- Restore global state with `t.Cleanup`, and `t.Setenv` rather than `os.Setenv`.
  Beware `sync.OnceValue` caching a decision across tests — swap the package
  var instead of relying on test order, and check `-shuffle=on` passes.

### Documentation

A user-visible change updates `README.md` in the same commit: the relevant
section, its table-of-contents entry, and the two command tables. Explain the
trap, not just the flag.
