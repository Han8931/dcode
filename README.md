<div align="center">

# meari-dcode

**Point it at a codebase. Decode it.**

![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)
![Terminal + Web](https://img.shields.io/badge/runs%20in-terminal%20%26%20browser-blueviolet)
![Local-first](https://img.shields.io/badge/local--first-your%20files-success)
![Offline OK](https://img.shields.io/badge/AI-optional%2C%20any%20provider-lightgrey)

</div>

---

## 📖 What it is

**meari-dcode** (from *decode*) is a local-first app for **understanding code**. Open a
real source tree in a fast three-pane workspace — file tree, editor, AI chat —
select a function or a whole file, and ask the model to **explain it**. The
explanation streams into the chat grounded in the actual code, and you can save
it as a markdown note for later.

```
┌ files ──────┐┌──────── editor ─────────┐┌──── chat / explain ──┐
│ ▾ internal  ││ func Open(dir string)…  ││ explains what this   │
│   core.go   ││   if err := os.Mkdir…   ││ does, step by step…  │
│ ▸ web       ││   return &Vault{…}      ││ > :decode this func  │
└─────────────┘└─────────────────────────┘└──────────────────────┘
```

It runs as a fast **terminal app** and a **local web app** — two thin front-ends
over one shared Go core, working on the same files.

## 💡 Why meari-dcode?

- 📁 **Your code stays yours.** Point it at any directory of source files. meari-dcode
  reads them — it never rewrites your source. Explanations are saved as separate
  markdown notes in a dedicated folder, so your repo stays clean.
- 🧩 **Context-aware explanations.** meari-dcode grounds each explanation in the
  surrounding file — and, when a single file isn't enough, pulls in the
  *definitions* it depends on (the symbols it references, defined elsewhere in the
  project) — so the answer reflects how the code actually fits together.
- 🧭 **Structural repo map.** meari-dcode parses your code into a ranked map of every
  file's signatures — via Go's own parser and **Tree-sitter** for Python,
  JavaScript, TypeScript/TSX, and Rust — and feeds it to `:overview`, `:explain`,
  and `:diff`. View it any time with `:map` (instant, no AI needed).
- ✍️ **Explanations become notes, not chat scroll.** Save a decode as a linked
  markdown note you own, edit, and revisit.
- 🪄 **Edit your notes with AI.** `:polish` / `:edit` rewrite a note (or a Visual
  selection); the proposal streams into the chat to review, then `:apply` or
  `:discard`.
- ⌨️ **A real modal editor** with Vim motions, visual mode, undo/redo, and syntax
  highlighting.
- 🔌 **Local-first and provider-agnostic.** Plug in OpenAI, a local Ollama model,
  or any OpenAI-compatible endpoint. Nothing leaves your machine except the model
  calls you configure.

## 🚀 Quick start

> **Prerequisite:** a C compiler (cgo) must be available — the multi-language repo
> map uses Tree-sitter, a C library. macOS (Xcode Command Line Tools) and most
> Linux (`gcc`/`clang`) setups already have one; `go install`/`go build` handle
> the rest.

Install it once, then run it anywhere:

```bash
go install .         # builds and drops `dcode` on your PATH (~/go/bin)
```

```bash
dcode                  # decode the current directory, in your terminal
dcode ~/code/project   # decode a specific project
dcode serve            # the same workspace, in your browser
dcode check            # verify your AI provider end-to-end
```

> Prefer not to install? `go build -o dcode .` and run `./dcode` from the repo.

### Opening a project

Run `dcode` inside a repo and it decodes **that repo** — no config needed. The
source tree is chosen most-specific first:

1. a path argument — `dcode ~/code/foo`
2. the configured `[vault] dir` (see below)
3. otherwise, the **current directory**

You can also switch projects without leaving the app: press **`,o`** (or run
**`:open <path>`**) for a picker that lists **recently opened projects** and lets
you type a new path. Your code is always read **read-only** — meari-dcode never
rewrites your source.

### Configuration (optional)

To set a default project and wire up an AI, copy the documented template and
edit what you need (everything is optional):

```bash
cp config.example.toml config.toml
```

```toml
# config.toml
[vault]
dir = "~/code/some-project"   # source tree to browse (default: current directory)

[ai]
provider = "ollama"           # or "openai" / any compatible endpoint
model = "llama3.1"
```

### Working inside the workspace

| You type | meari-dcode does |
|---|---|
| `:explain` · `:decode` | 🔍 explains the open file (or your Visual selection) in the chat |
| `,d` (in Visual mode) | 🔍 decode the selected lines — shortcut for `:decode` |
| `:overview` | 🗺️ a whole-project architecture overview, saved as an `OVERVIEW` note |
| `:map` | 🧭 a structural map of the repo — every file's signatures, ranked (instant, **no AI needed**) |
| `:diff` · `:diff main` | 🔀 explains your changes (`git diff`), saved as a note under `diffs/` |
| `:ask is this thread-safe?` | 💬 a grounded chat about the open file or selection |
| `,o` · `:open <path>` | 📂 switch to another project (recent list + path entry) |
| `,ff` · `,fg` | 🔎 fuzzy-find files / search contents |
| `:note` | 📝 saves the current explanation as a markdown note |
| `:polish` · `:edit make this tighter` | 🪄 an AI rewrite of a note, to review then `:apply` |

## 🧩 One core, two faces

The TUI and web UI stay in feature parity because neither contains business
logic — both drive the same headless Go engine (`internal/core`).

## 🌱 Status

In active development. meari-dcode began as a learning-vault app (*Meari*) and is being
refocused into the code-decoding tool described above.
