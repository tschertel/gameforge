# GameForge — notes for Claude

GameForge is a Go desktop app (Tk GUI via `modernc.org/tk9.0`, no CGO) for backups of games the user owns: Nintendo Switch NSP/NSZ merging and compression, disc images (CHD, XISO, RVZ, CSO/ZSO, Redump names), and ROMs (7z, No-Intro names, RetroAchievements check). Repository: https://github.com/tschertel/gameforge (MIT).

The user writes in Brazilian Portuguese; answer in Portuguese. Code, comments, commit messages, README.md and CHANGELOG.md are in English.

## Build and test

- Windows: `./build.ps1` (PowerShell) → `gameforge.exe`, built with `-H windowsgui`. Linux: `./build.sh`, or `CGO_ENABLED=0 GOOS=linux go build -ldflags '-s -w' -o gameforge .`
- macOS (experimental): `CGO_ENABLED=0 GOOS=darwin GOARCH=arm64` (or `amd64`) cross-compiles from Windows. There is no Mac to test on, so the macOS builds are compiled but never run; say so whenever they come up.
- Tests: `go vet ./... && go test ./...`. Optional tests run when an environment variable is set:
  - `SEVENZIP` points to `7z.exe` (7-Zip compatibility);
  - `CHDMAN` points to `chdman`;
  - `PKGTOOL` points to LibOrbisPkg's `PkgTool.exe` (or `PkgTool.Core.exe`), which builds real fake PS4 PKGs for the PFS extraction test; with `PS4KEYS` pointing to a `pkg.keys` holding the PS4 keysets, the test also extracts a package with another passcode (the keys stay outside the repository);
  - `REDUMP_DATS` and `NOINTRO_DATS` point to folders of real `.dat` files.
- Tk's dialog patches (Tcl run through `eval.Eval`) cannot be tested with `go test`: only the main package may initialize the eval extension. To check them, use a temporary file with `//go:build probe` holding an `init()` in package main, build with `-tags probe`, and delete the file afterwards.
- After building or probing, do not leave the app running; the user starts it from their own terminal and tests it themselves before a release.

## Layout

- Package main: one file per tab or feature. `tabs.go` holds the generic conversion-tab framework (`converter`); the tabs are `chd.go`, `xbox.go`, `rvz.go`, `ciso.go` and `sevenz.go`. Renaming lives in `rename.go` (Switch), `discrename.go` (Redump) and `romrename.go` (No-Intro). `main.go` holds the window, the Switch tab and the dialogs, `about.go` holds `appVersion`.
- `internal/`: the formats (`chd`, `rvz`, `xiso`, `ciso`, `sevenzip`, `nsz`, `nx`, `merge`), game data (`discinfo`, `redump`, `nointro`, `datfile`, `titledb`), `config` (`gameforge.json`) and `i18n`.
- `internal/i18n/messages.go`: every user-visible string, in five languages in this order: English, Portuguese, German, French, Spanish. A string used without arguments must not contain `%`.

## Conventions

- **Bump the version with every change to the program**, without being asked: set `appVersion` in `about.go` and extend the history comment above it, then update "Current version" in README.md and "Versão atual" in README.pt-BR.md. Use the minor number for features and the patch number for fixes and small UI tweaks. Unreleased changes share one version. A change that does not touch the program (docs only) does not bump the version.
- Keep README.md (English) and README.pt-BR.md in sync.
- CHANGELOG.md follows Keep a Changelog. Each release gets a section with its date and a link at the bottom.
- Commits use the user's private GitHub email `485568+tschertel@users.noreply.github.com`, never a work email. Commit and push only when the user asks.

## Release (when the user says to send a version)

1. Write the version's CHANGELOG section, and regenerate THIRD_PARTY_LICENSES.txt if dependencies changed.
2. Commit, push, then create an annotated tag `vX.Y.Z` and push it.
3. Build from the tag with `CGO_ENABLED=0` and `-trimpath`, for Windows (`-H windowsgui -s -w`), Linux (`-s -w`) and macOS arm64 and amd64 (`-s -w`, experimental).
4. Package the builds:
   - Windows: `gameforge-X.Y.Z-windows-amd64.zip` with `gameforge.exe`.
   - Linux: `gameforge-X.Y.Z-linux-amd64.tar.gz` with `gameforge`, mode 755.
   - macOS: `gameforge-X.Y.Z-macos-arm64.tar.gz` and `gameforge-X.Y.Z-macos-amd64.tar.gz` with `gameforge`, mode 755.
   - All, inside a `gameforge-X.Y.Z/` folder: README.md, README.pt-BR.md, CHANGELOG.md, LICENSE, THIRD_PARTY_LICENSES.txt, `keys/README.txt`.
   - Also write `SHA256SUMS.txt`.
5. Publish with `gh release create vX.Y.Z --repo tschertel/gameforge`, with notes taken from the changelog (downloads, the keys note, what's new). The macOS downloads are marked experimental and untested, with how to get past Gatekeeper (unsigned: System Settings → Privacy & Security → Open Anyway, or `xattr -d com.apple.quarantine gameforge`). The user handles GitHub and `gh` logins.

## Hard rules

- No keys anywhere: not in the code, the repository, the packages or the output. `prod.keys` and the Wii keys are the user's own, in `keys/` (git-ignored except `keys/README.txt`). Never print or publish them.
- The app contains no games. Do not help obtain games or ROMs.
- Do not copy GPL code, for example from Game Format Converter. Write an independent implementation.
- The downloaded databases (titledb, Redump and No-Intro from libretro-database, RetroAchievements game lists) live in the `cache/` folder next to `gameforge.json` and are never committed.
- The RetroAchievements web API key is the user's own: it is stored only in `gameforge.json` (`retroachievements_key`), sent only to RetroAchievements, and must never appear in logs, errors or commits. Do not use it yourself; the RetroAchievements site is blocked on the user's work network, so real checks are done by the user.
