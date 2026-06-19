# clihome

**List, inspect, create, and sync the config homes of AI coding CLIs** —
Claude Code, Codex, … (`~/.claude*`, `~/.codex*`), one home per account.

## Install

```bash
npm install -g clihome
```

This pulls a small launcher plus the prebuilt native binary for your platform
(macOS · Linux · Windows, arm64/x64). No Go toolchain required.

Then:

```bash
clihome                    # interactive table of every home, across tools
clihome list               # all ~/.claude*, ~/.codex*, … homes
clihome info claude2       # account + config details for one home
clihome new [tool]         # create the next free ~/.<tool><n>
clihome sync --from claude2 --to claude --status --diff
```

## Why

Running more than one account — or more than one tool — means juggling parallel
config homes (`CLAUDE_CONFIG_DIR=~/.claude2 claude`, `CODEX_HOME=~/.codex2 codex`).
They drift: your instructions, settings, skills, rules and memory end up
different in each. `clihome` lists every home (grouped by tool), shows whose
account each one is, and mirrors your setup from one onto another of the **same
tool** — safely, with a diff preview and backups.

## Commands

| Command | What it does |
|---|---|
| `clihome` | Interactive homes table — pick a home to sync, view, or delete; or add a new one. |
| `clihome list` (`ls`) | Every `~/.<tool>*` home with tool, account, model, last-active. |
| `clihome info [name]` | Account + config details for a home (provider-specific). |
| `clihome new [tool]` | Scaffold the next unused `~/.<tool><n>`, ready for a fresh login. |
| `clihome aliases` | Print shell aliases so each home launches as `claude2`, `codex2`, … |
| `clihome install` | Write those aliases into `~/.zshrc` (idempotent, backed up). |
| `clihome sync` | Mirror one home onto another of the same tool. |

## Other install methods

```bash
go install github.com/cchome/clihome/cmd/clihome@latest   # via Go
```

Full docs, sync semantics, and safety guarantees:
**https://github.com/cchome/clihome**

## License

MIT © clihome contributors
