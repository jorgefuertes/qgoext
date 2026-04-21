# qgoext

Zed extension that enriches `gopls` hover tooltips and code-lens entries with reference and implementation counts.

## What it does

When editing Go in Zed:

- **Hover**: tooltips gain a `5 references · 2 implementations` summary plus a clickable list — each entry links to the file at the right line, so you can jump to any reference or implementation directly from the tooltip.
- **Quick actions**: the code-lens menu (merged in Zed via PR #26848) gains entries such as `5 references` and `2 implementations` that show the count in the title.

> **Navigation note**: the quick-actions menu entries are display-only. Zed has no language-server command that opens the references panel, so selecting an entry is a no-op. Use the hover links (or Zed's built-in `editor: find all references`) to navigate.

Both features are fed by a small Go proxy that sits between Zed and the real `gopls`, intercepting only `textDocument/hover` and `textDocument/codeLens` and forwarding everything else unchanged.

## Status

Pre-alpha. MVP scope: functions, methods, structs, interfaces, top-level consts and vars.

## Requirements

- [gopls](https://pkg.go.dev/golang.org/x/tools/gopls) on `PATH` (`go install golang.org/x/tools/gopls@latest`)
- Zed 0.160 or later (for the code-lens quick-actions feature)

## Installation

Not yet published. For now: clone the repo and install as a dev extension via *Zed → Extensions → Install Dev Extension*.

Then add to `settings.json` so Zed stops launching the built-in gopls alongside qgoext's proxy (otherwise hovers appear duplicated):

```jsonc
{
  "languages": {
    "Go": {
      "language_servers": ["qgoext", "!gopls"]
    }
  }
}
```

## License

GPL-3.0-or-later. See [LICENSE](./LICENSE).
