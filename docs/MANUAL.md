# d-code Manual

**d-code** (from *decode*) is a local-first app for understanding code. Point it at a
source tree, open a file, and ask the AI to explain it — grounded in the whole file
and related project files. Explanations can be saved as markdown notes in a separate
folder, so your code repo stays clean.

- [Running d-code](#running-d-code)
- [Configuration](#configuration)
- [The workspace](#the-workspace)
- [Decoding code](#decoding-code)
- [Notes & AI editing](#notes--ai-editing)
- [The chat pane](#the-chat-pane)
- [The editor](#the-editor)
- [The command line](#the-command-line)
- [The web UI](#the-web-ui)
- [Project layout](#project-layout)
- [Notes & current limits](#notes--current-limits)

## Running d-code

```bash
go build -o dcode .

./dcode                         # the workspace, in your terminal
./dcode serve                   # the same workspace, in your browser
./dcode serve --addr :9000      # custom port
./dcode check                   # diagnose the AI provider connection
./dcode -vim / -default         # force Vim / non-Vim editor keybindings
```

A bare `./dcode` opens the three-pane terminal workspace over the configured source
tree. There is one mode — no wizard.

## Configuration

All configuration lives in `config.toml` next to the binary (or pass `-config <path>`).
Everything is optional; copy the template to start:

```bash
cp config.example.toml config.toml
```

### Source tree and notes

```toml
[vault]
# dir is the source/code tree d-code browses (read-only). Point it at any project.
# "~/" expands; a relative path is rooted at the dcode directory. Default: ./vault
dir = "~/code/some-project"

# notes_dir is where d-code SAVES explanations and notes (writable), kept separate
# from your source so the repo stays clean. Default: ./dcode-notes
# notes_dir = "~/dcode-notes"
```

d-code spans two roots in one tree:

- the **source root** (`dir`) — the code you're decoding. **Read-only**: source files
  open for reading but are never written to, and never gain note frontmatter.
- the **notes root** (`notes_dir`) — where explanations and any notes you write live.
  Writable, and physically separate from your source.

In the file tree they share one namespace, so a saved explanation
(`internal/core.go.md`) appears right next to the file it explains (`internal/core.go`)
— while living in the separate notes dir on disk.

### AI providers (OpenAI-compatible)

Every provider is reached through the OpenAI-compatible chat-completions API, so one
code path works for all — only the base URL / model / key differ.

**OpenAI**
```toml
[ai]
provider = "openai"
model = "gpt-4o-mini"
api_key_env = "OPENAI_API_KEY"   # the NAME of the env var holding your key…
# api_key = "sk-..."             # …or paste the key itself (env var wins)
```
```bash
export OPENAI_API_KEY=sk-...     # in the same shell you run dcode from
```

**Ollama (local, no key needed)**
```toml
[ai]
provider = "ollama"
model = "llama3.1"
# base_url defaults to http://localhost:11434/v1
```

**Any compatible gateway**
```toml
[ai]
provider = "compatible"
base_url = "https://your-gateway/v1"
model = "your-model"
api_key_env = "YOUR_KEY_ENV"   # optional — no-auth local servers work without a key
# timeout_seconds = 120        # raise for big/slow local models
```

**Diagnose your connection** with `dcode check` — it prints the resolved
provider/base URL/model/key status, verifies the model exists upstream, and sends a
real test request:

```
$ dcode check
d-code AI connection check
  provider:  ollama
  base url:  http://localhost:11434/v1
  model:     qwen3-coder-next:latest
  api key:   not set (looked in $OPENAI_API_KEY)

✓ provider reachable; model "qwen3-coder-next:latest" is available (7 models total)
✓ chat round-trip OK in 252ms
```

Without a provider, d-code runs **offline**: browsing and the editor work fully; the
AI features (`:explain`, `:ask`, `:polish`) explain that they need a provider instead
of failing.

### Layout & pane sizes

Set your default pane split with `sidebar_percent` / `chat_percent` under `[ui]`
(percent of the width; the editor takes the rest). `:compact` / `:wide` adjust the
editor/chat split live, and `sidebar_folded = true` starts with the file tree folded
away (`:fold` toggles it live).

## The workspace

Three panes:

```
┌ files ──────┐┌──────── editor ─────────┐┌──── chat / explain ──┐
│ ▾ internal  ││ func Open(dir string)…  ││ explains what this   │
│   core.go   ││   if err := os.Mkdir…   ││ does, step by step…  │
│ ▸ web       ││   return &Vault{…}      ││ > :decode this func  │
└─────────────┘└─────────────────────────┘└──────────────────────┘
```

- **Files (left)** — the source tree plus your saved notes, in one tree. `Ctrl-W h/l`
  (or `Tab`) moves focus between panes.
- **Editor (center)** — the open file. Source files are **read-only** (the header shows
  `CODE · … · read-only`); markdown notes are editable.
- **Chat (right)** — streamed explanations and an interactive, code-grounded chat.

**The file tree** mirrors the real on-disk structure. Source files (`.go`, `.py`, `.js`,
`.ts`, `.rs`, `.java`, `.c`, …) and your notes both appear. From the tree: `j/k` move,
`enter` opens, `r` reloads from disk, fold/unfold directories, and NERDTree-style node
ops on the notes (`Space` mark, `m` then **a**dd / **m**ove / **d**elete) — source is
read-only, so node ops only touch notes.

**Fuzzy find everywhere:** `,ff` jumps to any file by name, `,fg` greps every file's
contents, from any pane.

## Decoding code

This is the headline flow. Open a source file, then:

| You type | d-code does |
|---|---|
| `:explain` · `:decode` | explains the **open file** in the chat, grounded in the whole file and related project files |
| *(Visual selection)* `:decode` | explains just the **selected lines** |
| `,d` *(in Visual mode)* | shortcut for `:decode` on the selection |
| `:note` | saves the last explanation as a companion markdown note |
| `:map` | prints the **structural repository map** into the chat — instant, no AI needed |

**Context-awareness.** An explanation always sees the whole file, not just the lines in
view. d-code also builds **cross-file context**: a **structural repository map** of the
project (every source file's top-level signatures, ranked so the most-referenced code
leads) plus the referenced definitions the file depends on and the contents of its
directory neighbours (its package/module), so the explanation reflects how the code
connects — not a single file in isolation. The same map powers `:overview` and `:diff`,
and you can view it directly with `:map` (pure static analysis, so it works offline).

**Saving.** After `:explain`, the explanation streams into the chat and is parked; the
conversation is seeded so you can ask follow-ups (`:ask why is this needed?`) that stay
grounded. `:note` writes the explanation as `‹source-path›.md` under the notes dir — it
shows in the tree beside the source file, but lives in the separate notes folder, so
your code repo is never touched.

## Notes & AI editing

Notes (markdown) are fully editable. `Ctrl-S` saves; edits autosave as you go.

| You type | d-code does |
|---|---|
| `:new <title>` | create a new markdown note |
| `:ask <question>` | a grounded chat about the open file (or a Visual selection) |
| `:polish` · `:edit <instruction>` | AI rewrite of the open note (or a selection), streamed to review |
| `:apply` · `:discard` | accept (undoable) or drop a proposed rewrite |
| `:backlinks` | toggle the "linked mentions" panel for the open note |

`:polish`/`:edit` only apply to notes — source files are read-only (use `:explain`
instead). The proposal streams into the chat to read first; `:apply` writes it back (one
`u` undoes it), `:discard` drops it.

## The chat pane

Replies stream live and render markdown in color. `[[wikilinks]]` between notes are
clickable; per-note chat history is kept as you switch files.

- **Copy from the transcript:** drag to select, then `Alt-C` (works over SSH via OSC 52);
  `:copy` / `:yank` copies the last reply (or `:copy code` the last code block).
- **Paste:** `Alt-V` / `Cmd-V`, or `:paste`.
- **Export:** `:export` writes the transcript to the exports dir.
- **Stop a reply:** `Esc` while it streams.

## The editor

A modal Vim editor (or `-default` for plain keybindings): motions, counts, operators,
Visual mode, undo/redo, a jumplist (`Ctrl-O` / `Ctrl-I`), and syntax highlighting.

- **Highlighting:** full rules for Go and Python and markdown; other languages get
  generic highlighting (strings, numbers, comments). The language is chosen from the
  file's extension on open.
- **Read-only source:** opening a source file suspends autosave, so editing keys never
  persist to your source. Notes save normally.
- **Visual mode → AI:** select a span, then `:explain`/`:decode` (or `,d`) to decode it,
  `:ask` to discuss it, or `:polish`/`:edit` to rewrite it (notes only).

## The command line

`:` opens the command line. Commands:

`:explain` · `:decode` · `:note` · `:ask` · `:polish` · `:edit` · `:apply` · `:discard`
· `:new` · `:backlinks` · `:copy` · `:paste` · `:export` · `:fold` ·
`:compact` · `:wide` · `:q`

`Tab` completes command names.

## The web UI

```bash
./dcode serve                  # http://localhost:8765
./dcode serve --addr :9000     # custom port
```

The web UI serves the same engine as the terminal app — file tree, editor with markdown
preview, and a streaming AI chat — over `localhost`. Both front-ends drive the same Go
core, so they stay in feature parity.

## Project layout

```
main.go                 entry point (TUI default; `serve` / `check` subcommands)
internal/
  core/                 the headless engine: notes, search, chat, explain, two roots
    explain.go          :explain — context gathering + SaveExplanation
    roots.go            routes paths between the read-only source root and notes root
  vault/                the markdown + source-file store (read/list/walk)
  tutor/                the AI client (OpenAI-compatible); ExplainStream lives here
  editor/               the modal Vim editor + syntax highlighting
  tui/                  the terminal UI (file tree | editor | chat)
  web/                  the local web GUI (net/http) + `dcode serve`, over core
  config/               config loading + defaults
```

The rule that keeps the two front-ends in parity: **business logic lives in
`internal/core`, never in a UI handler.** New capabilities are wired behind core methods
so the TUI and web UI gain them at once.

## Notes & current limits

- **Read-only source.** d-code never writes to your source tree. All writing —
  explanations, notes, AI edits — goes to the separate notes dir.
- **Highlighting** ships full rules for Go and Python (plus markdown); other languages
  get generic highlighting. Adding a language is a small change in the editor.
- **Cross-file context** currently covers the project file map plus the target file's
  directory neighbours. Following imports for deeper context is a natural next step.
- **Project's own markdown** (a repo's `README.md`, etc.) is not browsed as a note —
  d-code shows source files from the source tree and markdown from the notes tree.
