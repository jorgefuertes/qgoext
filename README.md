# qgoext

Zed extension that enriches `gopls` hover tooltips and code-lens entries with reference and implementation counts.

## What it does

When editing Go in Zed:

- **Hover**: the tooltip gains a line with the reference and implementation counts. Each count is a clickable link that jumps to the first occurrence of its kind (Zed's hover renderer only navigates to a single file/line per link). To see every reference or implementation in a panel, use Zed's built-in `editor: find all references` / `editor: find all implementations`.

The feature is fed by a small Go proxy that sits between Zed and the real `gopls`, intercepting only `textDocument/hover` and forwarding everything else unchanged.

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
