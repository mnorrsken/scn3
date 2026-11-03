# Contributing

Thanks for helping. Bug reports, fixes and small features are welcome.

## Reporting a bug

Open an issue with the bug form. Include your OS and version, the scn3 version, how you installed it, and what you saw. If scanning goes wrong, run the command line tool on the same folder and paste its output:

```
scan <path>
```

It prints the first errors it hits, which usually shows the cause.

Security problems: please do not open a public issue. See [SECURITY.md](SECURITY.md).

## Before you write code

For anything bigger than a small fix, open an issue first so we can agree on the approach. scn3 runs on Windows, macOS and Linux, so a change in one OS layer can break another.

## Building and testing

You need Go (the version in `go.mod`) and Node.js. On Linux also `libgtk-3-dev` and `libwebkit2gtk-4.1-dev`.

| Command | What it does |
| --- | --- |
| `make run` | Starts the app with live reload of the UI |
| `make build` | Builds the app into `build/bin` |
| `make build-cli` | Builds the `scan` command line tool into `bin/` |
| `make test` | Runs the Go and UI tests |
| `make race` | Runs the Go tests with the race detector |
| `make lint` | Runs golangci-lint |
| `make fmt` | Formats the Go code |

## Checking what a scan misses

`tools/scancheck` compares the app's scan with a slower, separate walk of the same folder and lists every difference with a reason and whether it is fine to ignore:

```sh
go run ./tools/scancheck app <path> > app.jsonl            # as your normal user
sudo go run ./tools/scancheck compare <path> app.jsonl > report.md   # best as admin
```

On Windows, run the second command in a terminal opened with **Run as administrator**. The tool is not part of any release.

## Code layout

- `internal/platform`: everything that differs per OS, one file per OS (`_windows.go`, `_linux.go`, `_darwin.go`).
- `internal/scanner` and `internal/tree`: the parallel scan and the compact tree. No UI code.
- `internal/desktop`: the API the UI calls.
- `frontend/src`: the TypeScript UI. `layout.ts` has no DOM code and is unit tested.
- `cmd/scan`: the command line tool.

## Pull requests

- One change per pull request, with tests for new behaviour.
- `make test` and `make lint` pass, and Go code is `gofmt` clean. CI runs both on Windows, macOS and Linux.
- Add a line to the `## [Unreleased]` section of `CHANGELOG.md` for anything a user would notice.
- Keep the standard library first; a new dependency needs a reason in the pull request.
- Commit messages: a short subject in the imperative ("Fix X", "Add Y").

By contributing you agree that your work is licensed under the [Apache License 2.0](LICENSE), the same as the project.
