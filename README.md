# Murmur

Murmur captures a rough thought, works out where it belongs in your Obsidian
vault, formats it as clean Markdown, shows you exactly what will be written, and
only then writes it.

It is a terminal application, it is local-first, and it never sends your vault
anywhere.

```
╭─ Murmur ───────────────────────────────────────────────────────────╮
│                                                                    │
│   Investigate why the Z13 trackpad is detected as a fallback       │
│     mouse and whether hid_asus needs to be patched.                │
│                                                                    │
├─ Suggested routing ────────────────────────────────────────────────┤
│                                                                    │
│ ▸ Note        Projects/Linux/ROG Flow Z13.md ← → 2 alternatives    │
│   Section     Trackpad troubleshooting                             │
│   Format      Task                                                 │
│   Confidence   87%  ▰▰▰▰▰▰▰▰▰▱                                     │
│   Tags        #linux #asus #z13 #trackpad                          │
│                                                                    │
│   Why: matched note name "ROG Flow Z13", tags: linux               │
│                                                                    │
│     · Reference/Fedora Suspend.md                                  │
│     · Inbox/Tasks.md                                               │
│                                                                    │
├─ Preview ──────────────────────────────────────────────────────────┤
│                                                                    │
│   - [ ] Investigate why the Z13 trackpad is detected as a          │
│     fallback mouse and whether `hid_asus` needs to be patched.     │
│     - Added: 2026-08-05                                            │
│                                                                    │
╰────────────────────────────────────────────────────────────────────╯

 enter save   tab fields   space search notes   ctrl+e edit preview   esc back   q quit
```

## What it does

You type something half-formed. Murmur decides:

- **which note** it belongs in,
- **which heading** inside that note,
- **what kind of thing** it is (task, idea, journal entry, question, bookmark …),
- **which tags** apply,
- and **what Markdown** to write.

Then it shows you all five and waits. Nothing is written until you press Enter.

## Installation

Murmur is a single static binary. It needs Go 1.25 or newer to build (the
pure-Go SQLite driver requires it) and has no cgo dependency, so
`CGO_ENABLED=0` works.

```bash
go install github.com/alliebayless/murmur@latest
```

Or from a clone:

```bash
git clone https://github.com/alliebayless/murmur
cd murmur
go build -o murmur .
sudo install -m 0755 murmur /usr/local/bin/murmur
```

## First run

```bash
murmur
```

The setup screen asks for your vault. It looks in the usual places
(`~/Documents`, `~/Notes`, `~/Dropbox`, …) for a directory containing
`.obsidian` and offers what it finds, so usually you can press Enter.

Configuration is written to `$XDG_CONFIG_HOME/murmur/config.yaml`
(normally `~/.config/murmur/config.yaml`). The index and history database lives
in `$XDG_DATA_HOME/murmur/murmur.db` (normally `~/.local/share/murmur/`).

Murmur then indexes the vault. A few thousand notes take well under a second,
and later runs only re-read files whose modification time or size changed.

## Usage

```bash
murmur                                        # open the capture window
murmur "Add barcode scanning to the pantry app"
echo "Research the Fedora suspend problem" | murmur
murmur --daily "Fixed the Z13 trackpad problem today"
murmur --quick "Buy a replacement UPS battery"

murmur index                 # refresh the vault index
murmur index --rebuild       # discard it and re-parse everything
murmur history               # recent captures
murmur history --plain -n 50 # …as plain text, for scripts
murmur undo                  # reverse the last write, after confirmation
murmur learning reset        # forget learned routing associations
murmur version
```

Flags:

| Flag | Meaning |
| --- | --- |
| `-q`, `--quick` | Save without confirmation when routing is confident enough |
| `-d`, `--daily` | Route to today's daily note |
| `-t`, `--type` | Force a content type (`task`, `idea`, `journal`, `project`, `reference`, `question`, `bookmark`, `note`) |
| `--no-ai` | Skip the AI provider for this capture |
| `-v`, `--verbose` | Log routing decisions and timings to stderr |
| `--config`, `--database` | Override file locations |

### Piping

`echo "…" | murmur` works: Murmur reads the thought from standard input and then
reads your keystrokes from `/dev/tty`, so the confirmation screen still appears.
If there is no terminal at all (a cron job, say), Murmur prints its suggestion
and exits non-zero rather than writing something you have not seen — unless you
passed `--quick` and it is confident.

## Keybindings

**Capture**

| Key | Action |
| --- | --- |
| `enter` | Route the thought |
| `ctrl+j` / `alt+enter` | Insert a newline |
| `ctrl+h` | History |
| `esc` | Clear, or quit if already empty |
| `ctrl+c` | Quit |

Pasted multi-line text keeps its line breaks.

**Routing**

| Key | Action |
| --- | --- |
| `enter` | Save |
| `tab` / `shift+tab` | Move between fields |
| `space` | Open the note or heading picker (on those fields) |
| `←` `→` | Cycle alternatives (Note) or content type (Format) |
| `e` | Edit tags (on the Tags field) |
| `ctrl+e` | Edit the Markdown by hand |
| `esc` | Back to the capture box, text intact |
| `q` | Quit without saving |

**Pickers** — type to fuzzy search, `↑`/`↓` to move, `enter` to choose, `esc` to
go back. If nothing matches, Enter uses what you typed as a new note or heading.

**Preview editor** — `ctrl+s` apply, `ctrl+r` regenerate from the thought, `esc`
discard.

**After saving** — `o` open in Obsidian, `u` undo, `n` capture another, `q` quit.

## Routing hints

You can tell Murmur where something goes, inline. Hints are removed from the
saved text.

```text
@tidemail Add an attachment preview
#journal Finally fixed the trackpad issue
>Projects/Linux/ROG Flow Z13 investigate hid_asus
>"Inbox/Some Note.md" buy milk
Fix the fan curve #linux #hardware
```

- `@name` — a project or note. Matched against file names, titles and aliases.
- `#task`, `#idea`, `#journal`, `#question`, `#bookmark`, `#reference`,
  `#project`, `#note` — the content type.
- Any other `#word` — a suggested tag.
- A leading `>path` — an exact destination. Paths with spaces work when the note
  already exists, or when the path ends in `.md`; quote it otherwise.

Explicit hints always beat automatic routing, and give 100% confidence.

## How routing works

Four stages, in order. The first one that produces a destination wins, but
ranking always runs so alternatives are available in the UI.

1. **Explicit hints** — the syntax above, or `--daily`.
2. **Deterministic rules** — keyword and type rules from
   `~/.config/murmur/routes.yaml`. See [`example-routes.yaml`](example-routes.yaml).
3. **Vault ranking** — a weighted score over the index: exact name matches, tag
   overlap, heading matches, folder names, keyword overlap with a short excerpt,
   your past choices, wikilink relationships, and recency as a light
   tie-breaker. Returns the top three with a normalised confidence.
4. **AI (optional, off by default)** — see below.

If nothing matches, Murmur falls back by type: tasks and questions to
`default_task_note`, journal entries to the daily note, everything else to
`default_inbox`.

### Content types

| Input | Output |
| --- | --- |
| `remember to update forgejo` | `- [ ] Update Forgejo`<br>`  - Added: 2026-08-05` |
| `maybe tidemail should support newsletters` | `> [!idea]`<br>`> Tidemail should support newsletters.` |
| `today i finally fixed the z13 trackpad` | `- Fixed the Z13 trackpad.` |
| `why does nvidia resume fail on fedora` | `- [ ] Research: why does NVIDIA resume fail on Fedora?` |
| `https://example.com useful article about bubble tea` | `- [Useful article about Bubble Tea](https://example.com)` |

Note the capitalisation of *Forgejo*, *NVIDIA* and *Bubble Tea*. Murmur builds a
vocabulary from your own vault — note titles, aliases and tags — and restores
that spelling for words you typed in lower case. It gets better as your vault
grows, and it needs no AI.

## Configuration

Full annotated example: [`example-config.yaml`](example-config.yaml).

```yaml
vault_path: /home/allie/Documents/Obsidian

default_inbox: Inbox.md
default_task_note: Inbox/Tasks.md
daily_note_path: Daily/{{date}}.md
daily_template_path: Templates/Daily.md

date_format: "2006-01-02"
time_format: "15:04"

quick_mode_confidence: 0.90

excluded_paths:
  - .obsidian
  - .git
  - Templates
  - Attachments

ai:
  provider: none
  model: ""
  base_url: ""
  api_key_env: MURMUR_API_KEY

formatting:
  include_capture_date: true
  use_callouts_for_ideas: true
  task_date_property: Added
```

`{{date}}`, `{{year}}`, `{{month}}`, `{{day}}`, `{{time}}` and `{{weekday}}`
work in daily note paths, rule destinations and templates.

## Daily notes

```bash
murmur --daily "Finished the first Murmur prototype"
```

The note is created if missing, from `daily_template_path` when one is
configured:

```markdown
---
date: {{date}}
tags:
  - daily
---

# {{date}}

## Journal

## Tasks

## Notes
```

Journal entries go under `## Journal`, tasks and questions under `## Tasks`,
everything else under `## Notes`. The headings are configurable under
`daily_sections`.

## Quick mode

```bash
murmur --quick "Buy replacement UPS battery"
Saved to Inbox/Tasks.md under ## Hardware
```

Quick mode skips the confirmation screen **only** when you gave an explicit
destination (or `--daily`), or when confidence is at or above
`quick_mode_confidence`. Otherwise the normal screen opens, so an uncertain
guess never gets written silently.

## Writing to your vault

Murmur is careful with your files:

- **Atomic writes.** Content goes to a temporary file in the same directory,
  permissions are preserved, then it is renamed over the original. A crash
  cannot leave a half-written note.
- **Section-aware insertion.** Content goes at the end of the target section —
  before the next heading of the same or higher level — not at the end of the
  file. Deeper subheadings stay inside their parent section.
- **Frontmatter is never touched.** Neither are your line endings: a CRLF note
  stays CRLF.
- **Conflict detection.** The file is hashed when the preview is built and
  re-checked before writing. If it changed in between, Murmur refuses, rebuilds
  the preview and tells you.
- **Never outside the vault.** Every path is resolved and checked, including
  symlinks. `../`, absolute paths and traversal are rejected.

## Undo

Every write records the destination, the hashes before and after, the exact
inserted text, the insertion point, and a full backup of the previous content.

```bash
murmur undo
```

It shows you what will be reversed and asks. Then:

- If the note is **untouched** since Murmur wrote it, the previous content is
  restored exactly — or the note is deleted, if Murmur created it.
- If the note **changed afterwards**, Murmur will not roll back over your newer
  work. It offers to remove only the block it inserted, and says so.
- If the inserted block is **gone entirely**, Murmur refuses and explains, rather
  than guessing.

## Obsidian integration

After saving, press `o` to open the note in Obsidian. The URI is built from the
vault name and relative path. Obsidian does not need to be running — or
installed — for capture and storage to work; Murmur only ever touches Markdown
files on disk. It is not a plugin.

## AI providers

**Murmur ships with `provider: none` and is complete without AI.** All the
routing, formatting and type detection above is deterministic.

If you want a model's opinion:

```yaml
# Local, nothing leaves the machine
ai:
  provider: ollama
  model: llama3.1
  base_url: http://localhost:11434
```

```yaml
# Any OpenAI-compatible endpoint
ai:
  provider: openai
  model: gpt-4o-mini
  base_url: https://api.openai.com/v1
  api_key_env: MURMUR_API_KEY
```

```bash
export MURMUR_API_KEY=...   # never stored in the config file
```

The request contains **only**: the thought, the paths, titles, tags and
headings of at most four candidate notes, the list of content types, and
Murmur's own suggestion. Providers must return a single JSON object, and every
field is validated before use — the path must be plausible and inside the vault,
the type must be known, the Markdown must be non-empty, under 4 KB, and must not
contain frontmatter or a top-level heading. Anything that fails validation, or
any network error, is logged and discarded: you get the deterministic answer
instead. `--no-ai` skips the call for one capture.

## Privacy

- Your vault is **never** uploaded, indexed remotely, or sent anywhere.
- With the default `provider: none`, Murmur makes **no network requests at all**.
- The index stores paths, titles, aliases, tags, headings and a 600-byte excerpt
  per note — not full note bodies.
- The database is local, in your XDG data directory.
- API keys are read from environment variables and never written to disk by
  Murmur.
- "Learning" is a weighted table of words you have routed to each note. It is
  not a model, it never leaves your machine, and `murmur learning reset` clears
  it.

## Learning from corrections

When you change a suggested destination, heading or type, Murmur records which
words led to which note and weights corrections higher than confirmations. Next
time, similar words nudge ranking towards where you actually put things. It is a
nudge, not a rule: explicit hints and deterministic rules always win.

## Errors

Murmur explains problems instead of printing stack traces: a missing or
read-only vault, a read-only destination file, malformed frontmatter (indexed
anyway, with a warning), an unreachable AI provider, a locked or corrupted
database, a file that changed during preview, a destination outside the vault,
an empty thought, a terminal that is too small. Run with `--verbose` to see
routing decisions, index timings and provider errors on stderr.

## Development

```bash
go mod tidy
go fmt ./...
go vet ./...
go test ./...
go build ./...
```

Layout:

```
cmd/          Cobra commands
internal/
  app/        wiring: prepare, save, undo, history
  config/     YAML config in the XDG directory
  database/   SQLite plus migrations
  formatter/  thought -> Markdown, content classification
  indexer/    vault scanning
  markdown/   frontmatter, headings, insertion
  model/      shared domain types
  obsidian/   obsidian:// URIs
  provider/   optional AI classifiers
  router/     hints, rules, ranking, fuzzy search
  storage/    atomic, sandboxed vault file access
  tui/        Bubble Tea screens
migrations/   embedded SQL
testdata/     a small sample vault used by the tests
```

The domain packages have no knowledge of SQLite or Bubble Tea, which is what
keeps the tests fast: they run against strings and temporary directories. Tests
cover frontmatter parsing, heading extraction, insertion under and around
headings, daily note expansion, hint parsing, rule matching, ranking, path
traversal, atomic writes, undo including conflicts, AI response validation,
quick-mode thresholds, and the TUI event loop end to end.

## Current limitations

- **No filesystem watcher.** The index refreshes by comparing modification time
  and size at startup and after each save. Notes changed by another program
  mid-session are picked up on the next run.
- **Question phrasing is mechanical.** `why does X fail` becomes
  `Research: why does X fail?` rather than being rewritten into a statement.
  Deterministic code will not conjugate verbs; enable an AI provider if you want
  that.
- **Proper-noun casing comes from your vault.** Words your vault has never seen
  are left as you typed them.
- **One insertion point per capture.** Murmur writes one block to one note.
- **Undo is one step at a time**, most recent first.
- **Wikilink relationships are a small bonus**, not a full graph traversal.
- **Tags are suggestions**, not written into the note by default.

## Licence

MIT. See [LICENSE](LICENSE).
