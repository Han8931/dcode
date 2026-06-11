<div align="center">

# d-code

**Point it at a codebase. Decode it.**

![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)
![Terminal + Web](https://img.shields.io/badge/runs%20in-terminal%20%26%20browser-blueviolet)
![Local-first](https://img.shields.io/badge/local--first-your%20files-success)
![Offline OK](https://img.shields.io/badge/AI-optional%2C%20any%20provider-lightgrey)

</div>

---

## 📖 What it is

**d-code** (from *decode*) is a local-first app for **understanding code**. Open a
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

## 💡 Why d-code?

- 📁 **Your code stays yours.** Point it at any directory of source files. d-code
  reads them — it never rewrites your source. Explanations are saved as separate
  markdown notes in a dedicated folder, so your repo stays clean.
- 🧩 **Context-aware explanations.** d-code grounds each explanation in the
  surrounding file — and, when a single file isn't enough, reaches across related
  files in the project — so the answer reflects how the code actually fits together.
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

```bash
go build -o dcode .

./dcode            # the workspace, in your terminal
./dcode serve      # the same workspace, in your browser
./dcode check      # verify your AI provider end-to-end
```

Point it at the code you want to understand, and wire up an AI. Copy the
documented template and edit what you need (everything is optional):

```bash
cp config.example.toml config.toml
```

```toml
# config.toml
[vault]
dir = "~/code/some-project"   # the source tree to browse (default: ./vault)

[ai]
provider = "ollama"           # or "openai" / any compatible endpoint
model = "llama3.1"
```

Then, inside the workspace:

| You type | d-code does |
|---|---|
| `:explain` · `:decode` | 🔍 explains the open file (or your Visual selection) in the chat |
| `,d` (in Visual mode) | 🔍 decode the selected lines — shortcut for `:decode` |
| `:note` | 📝 saves the current explanation as a markdown note |
| `:ask is this thread-safe?` | 💬 a grounded chat about the open file or selection |
| `:polish` · `:edit make this tighter` | 🪄 an AI rewrite of a note, to review then `:apply` |

## 🧩 One core, two faces

The TUI and web UI stay in feature parity because neither contains business
logic — both drive the same headless Go engine (`internal/core`).

## 🌱 Status

In active development. d-code began as a learning-vault app (*Meari*) and is being
refocused into the code-decoding tool described above.
