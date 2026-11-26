# Contributing to GameForge

Bug reports and pull requests are welcome. GameForge is a hobby project with one maintainer, so answers can take a few days. Taking part means following the [code of conduct](CODE_OF_CONDUCT.md).

## Reporting a bug

Open an issue with the **Bug report** form: the version (About window), your system, the tab, what happened and the lines from the message box at the bottom of the window. Never attach keys, games or ROMs, and remove personal paths from those lines. Security problems go through [SECURITY.md](SECURITY.md), not public issues.

## Building and testing

You need Go (the version in `go.mod`); no C compiler, as Tk is used without CGO.

- Windows: `./build.ps1` builds `gameforge.exe`. Linux: `./build.sh`.
- Tests: `go vet ./... && go test ./...`. Some tests also run with real tools or data when an environment variable is set: `SEVENZIP` (path to `7z.exe`), `CHDMAN` (path to `chdman`), `REDUMP_DATS` and `NOINTRO_DATS` (folders of `.dat` files).

GUI regression tests open isolated windows and require an active desktop session: `GAMEFORGE_GUI_TESTS=1 go test . -run '^TestGUI$' -count=1`. They do not load user configuration, keys or games.

## Rules for changes

- **No keys and no games**, anywhere: not in code, tests, test data, issues or pull requests. Test data is synthetic.
- **No GPL code.** GameForge is MIT; write your own implementation instead of copying from GPL projects.
- Code, comments, commit messages, README.md and CHANGELOG.md are in English.
- Every user-visible string goes into `internal/i18n/messages.go`, in English, Portuguese, German, French and Spanish, in that order. If you don't speak one of them, say so in the pull request.
- Add a test for new format code, parsers and anything that writes or deletes files.
- Keep `go vet ./...` and `go test ./...` passing, and code formatted with `gofmt`.
- Describe the change in the pull request; the maintainer updates the version and the changelog when releasing.
