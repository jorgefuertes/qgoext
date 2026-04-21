# qgoext

Zed extension that enriches `gopls` hover tooltips and code-lens entries with reference and implementation counts.

## What it does

When editing Go in Zed:

- **Hover**: tooltips gain a line like `5 references · 2 implementations` under the usual hover content.
- **Quick actions**: the code-lens menu (merged in Zed via PR #26848) gains entries such as `5 references` and `2 implementations` that jump to the matching location when selected.

Both features are fed by a small Go proxy that sits between Zed and the real `gopls`, intercepting only `textDocument/hover` and `textDocument/codeLens` and forwarding everything else unchanged.

## Status

Pre-alpha. MVP scope: functions, methods, structs, interfaces, top-level consts and vars.

## Requirements

- [gopls](https://pkg.go.dev/golang.org/x/tools/gopls) on `PATH` (`go install golang.org/x/tools/gopls@latest`)
- Zed 0.160 or later (for the code-lens quick-actions feature)

## Installation

Not yet published. For now: clone the repo and install as a dev extension via *Zed → Extensions → Install Dev Extension*.

## License

GPL-3.0-or-later. See [LICENSE](./LICENSE).
