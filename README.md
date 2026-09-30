# Loom CLI

**Your Loom library as a local, searchable archive: grep what you said, back up everything, and recap a week in one command.**

loom-pp-cli signs in with your existing Loom browser session and syncs your library into a local SQLite store. hydrate pulls every transcript and Loom's own AI summaries and chapters, grep finds the exact moment you said something with a deep link, digest rolls a week into one recap, and activity shows new views and comments since you last looked.

Learn more at [Loom](https://www.loom.com).

Created by [@cmartinez9](https://github.com/cmartinez9) (cmartinez9).

## Install

The recommended path installs both the `loom-pp-cli` binary and the `pp-loom` agent skill (Claude Code, Codex, Cursor, Gemini CLI, GitHub Copilot, and other agents supported by the upstream [`skills`](https://github.com/vercel-labs/skills) CLI) in one shot:

```bash
npx -y @mvanhorn/printing-press-library install loom
```

For CLI only (no skill):

```bash
npx -y @mvanhorn/printing-press-library install loom --cli-only
```

For skill only — installs the skill into the same agents as the default command above, but skips the CLI binary (use this to update or reinstall just the skill):

```bash
npx -y @mvanhorn/printing-press-library install loom --skill-only
```

To constrain the skill install to one or more specific agents (repeatable — agent names match the [`skills`](https://github.com/vercel-labs/skills) CLI):

```bash
npx -y @mvanhorn/printing-press-library install loom --agent claude-code
npx -y @mvanhorn/printing-press-library install loom --agent claude-code --agent codex
```

### Without Node (Go fallback)

If `npx` isn't available (no Node, offline), install the CLI directly via Go (requires Go 1.26.6 or newer):

```bash
go install github.com/mvanhorn/printing-press-library/library/productivity/loom/cmd/loom-pp-cli@latest
```

This installs the CLI only — no skill.

### Pre-built binary

Download a pre-built binary for your platform from the [latest release](https://github.com/mvanhorn/printing-press-library/releases/tag/loom-current). On macOS, clear the Gatekeeper quarantine: `xattr -d com.apple.quarantine <binary>`. On Unix, mark it executable: `chmod +x <binary>`.

<!-- pp-hermes-install-anchor -->
## Install for Hermes

Install the CLI binary first. The installer writes binaries to a per-user managed bin directory by default: `$HOME/.local/bin` on macOS/Linux and `%LOCALAPPDATA%\Programs\PrintingPress\bin` on Windows.

```bash
npx -y @mvanhorn/printing-press-library install loom --cli-only
```

Then install the focused Hermes skill.

From the Hermes CLI:

```bash
hermes skills install mvanhorn/printing-press-library/cli-skills/pp-loom --force
```

Inside a Hermes chat session:

```bash
/skills install mvanhorn/printing-press-library/cli-skills/pp-loom --force
```

Restart the Hermes session or gateway if the newly installed skill is not visible immediately.

## Install for OpenClaw
Install both the CLI binary and the focused OpenClaw skill. The installer defaults binaries to a per-user bin directory (`$HOME/.local/bin` on macOS/Linux, `%LOCALAPPDATA%\Programs\PrintingPress\bin` on Windows):

```bash
npx -y @mvanhorn/printing-press-library install loom --agent openclaw
```

Restart the OpenClaw session or gateway if the newly installed skill is not visible immediately.

## Use with Claude Desktop

This CLI ships an [MCPB](https://github.com/modelcontextprotocol/mcpb) bundle — Claude Desktop's standard format for one-click MCP extension installs (no JSON config required).

The bundle reuses your local browser session — set it up first if you haven't:

```bash
loom-pp-cli auth login --chrome
```

To install:

1. Download the `.mcpb` for your platform from the [latest release](https://github.com/mvanhorn/printing-press-library/releases/tag/loom-current).
2. Double-click the `.mcpb` file. Claude Desktop opens and walks you through the install.

Requires Claude Desktop 1.0.0 or later. Pre-built bundles ship for macOS Apple Silicon (`darwin-arm64`) and Windows (`amd64`, `arm64`); for other platforms, use the manual config below.

<details>
<summary>Manual JSON config (advanced)</summary>

If you can't use the MCPB bundle (older Claude Desktop, unsupported platform), install the MCP binary and configure it manually.


```bash
go install github.com/mvanhorn/printing-press-library/library/productivity/loom/cmd/loom-pp-mcp@latest
```

Add to your Claude Desktop config (`~/Library/Application Support/Claude/claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "loom": {
      "command": "loom-pp-mcp"
    }
  }
}
```

</details>

## Authentication

Loom has no public API for your own videos. This CLI uses the same session cookie the Loom web app uses (connect.sid, valid about 30 days). Run auth login --chrome while signed in to loom.com in Chrome (it needs a cookie extraction tool such as pycookiecheat; doctor reports which one it found), or set LOOM_COOKIE='connect.sid=<value>' (a bare value is also accepted), then check it with auth status. When the session expires, commands say so and read-only commands fall back to the local store. Transcripts, captions and chapters of public videos work without a session; AI summaries need one.

## Quick Start

```bash
# Checks the install without touching the network.
loom-pp-cli doctor --dry-run

# Imports your Loom session from Chrome.
loom-pp-cli auth login --chrome

# Caches your recent videos locally.
loom-pp-cli sync --resources videos --since 30d

# Fetches transcripts, summaries and chapters for those videos.
loom-pp-cli hydrate --since 30d

# Finds every moment you said it, with timestamp links.
loom-pp-cli grep "onboarding"

```

## Unique Features

These capabilities aren't available in any other tool for this API.

### Your library as a searchable archive
- **`grep`** — Find the exact moment you said something in any hydrated video, with a link that jumps straight to it.

  _Reach for this when an agent must cite or reuse a past spoken explanation instead of re-watching recordings._

  ```bash
  loom-pp-cli grep "pricing experiment" --agent
  ```
- **`hydrate`** — Pull transcripts plus Loom's own summaries, chapters and action items for every video sync has stored, in one resumable pass.

  _Run this before grep or digest so the local text corpus is complete._

  ```bash
  loom-pp-cli hydrate --since 90d --agent
  ```

### Follow-ups and recaps
- **`activity`** — See which recent videos got new views, comments or reactions since your last check (the first run records the baseline), and which comments still need a reply.

  _Use this for follow-ups driven by who watched or commented, without opening videos one at a time._

  ```bash
  loom-pp-cli activity --since 30d --agent
  ```
- **`digest`** — One markdown recap of a week's videos with Loom's summaries, chapter links and action items.

  _Use this to build weekly notes or a status update from recorded standups and walkthroughs._

  ```bash
  loom-pp-cli digest --since 7d
  ```

## Recipes

### Find where you explained something

```bash
loom-pp-cli grep "funnel" --agent --select video_name,timestamp,link
```

Returns only the video, position and deep link for each hit. Needs transcripts in the local store: run sync, then hydrate, first.

### Friday recap

```bash
loom-pp-cli digest --since 7d
```

Markdown of the week's videos with summaries, chapters and action items, read from the local store after sync and hydrate.

### Who watched what I sent

```bash
loom-pp-cli activity --since 14d --agent
```

New views, comments and reactions since the last check, with needs_reply. The first run records a baseline, so deltas start on the second run.

### Back up the whole library

```bash
loom-pp-cli download --all --out ./loom-backup
```

Resumable MP4 backup of every video; rerun to pick up where it stopped. Pair with videos export --all --out ./loom-export for markdown transcripts and summaries.

### Preview a write before applying it

```bash
loom-pp-cli videos archive latest --dry-run --agent
```

Prints the exact ArchiveVideos operation and variables without changing anything. Rerun with --yes instead of --dry-run to apply; filters like --before 180d preview a bulk selection the same way.

## Usage

Run `loom-pp-cli --help` for the full command reference and flag list.

## Paths & environment variables

This CLI separates local files into four path kinds:

| Kind | Contents |
|------|----------|
| `config` | User-editable settings such as `config.toml` and saved profiles |
| `data` | Durable local data: `credentials.toml`, `data.db`, cookies, browser-session proof files, and other auth sidecars |
| `state` | Runtime state such as persisted queries and `teach.log` |
| `cache` | Regenerable HTTP/cache files |

Each kind resolves independently. The ladder is:

1. Per-kind env var: `LOOM_CONFIG_DIR`, `LOOM_DATA_DIR`, `LOOM_STATE_DIR`, or `LOOM_CACHE_DIR`
2. `--home <dir>` for this invocation
3. `LOOM_HOME` for a flat relocated root
4. XDG env vars: `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_STATE_HOME`, `XDG_CACHE_HOME`
5. Platform defaults matching existing installs

For containers and agent sandboxes, prefer a single relocated root:

```bash
export LOOM_HOME=/srv/loom
loom-pp-cli doctor
```

Under `LOOM_HOME=/srv/loom`, the four dirs resolve to `/srv/loom/config`, `/srv/loom/data`, `/srv/loom/state`, and `/srv/loom/cache`.

MCP servers do not receive CLI flags from the host. Put relocation in the host `env` block:

```json
{
  "mcpServers": {
    "loom": {
      "command": "loom-pp-mcp",
      "env": {
        "LOOM_HOME": "/srv/loom"
      }
    }
  }
}
```

Precedence matters in fleets: an ambient per-kind variable such as `LOOM_DATA_DIR` overrides an explicit `--home` for that kind. Use `LOOM_HOME` or the per-kind variables for durable fleet relocation; treat `--home` as the weaker per-invocation lever.

Relocation is one-way. Unsetting `LOOM_HOME` does not move files back to platform defaults, and `doctor` cannot find credentials left under a former root. Move the files manually before unsetting relocation variables.

Existing installs keep working because the platform-default rung matches the legacy layout. On the first auth write, stored secrets leave `config.toml` and are consolidated into `credentials.toml` under the data directory. Run `loom-pp-cli doctor --fail-on warn` to check path and credential-location warnings in automation.

## Commands

Every video argument accepts a 32-hex id, a loom.com share URL, or `latest` for your newest recording. Commands marked *(write)* change your Loom library; see [Agent Usage](#agent-usage) for the `--dry-run` / `--yes` rules. Run `loom-pp-cli videos --help` (or any other group) for subcommands and flags.

### videos

- **`loom-pp-cli videos list`** - List videos in your library, newest first (`--since`, `--match`, `--folder`, `--archived`, `--all`)
- **`loom-pp-cli videos get <video>`** - One video's metadata, pin and follow state
- **`loom-pp-cli videos search <query>`** - Server-side search for videos, folders, spaces, tags, people and playlists
- **`loom-pp-cli videos export [video]...`** - Save a Markdown (or JSON) bundle per video: metadata, summary, chapters, comments and transcript (by id, `--from-file`, or `--all`, `--since`, `--before`, `--folder`, `--match`)
- `videos rename`, `videos describe`, `videos settings` (downloads and comments on/off; needs `--yes`) *(write)*
- `videos archive`, `videos unarchive`, `videos move`, `videos share` (by id, `--from-file`, or `--all`, `--since`, `--before`, `--folder`, `--match`; need `--yes`), `videos delete`, `videos recover`, `videos duplicate` *(write)*
- `videos pin`, `videos follow`, `videos watch-later` *(write)*

### Per-video reads

- **`loom-pp-cli transcript [video]`** - Transcript as timestamped text, text, JSON, SRT or VTT
- **`loom-pp-cli captions [video]`** - WebVTT captions exactly as Loom serves them
- **`loom-pp-cli summary [video]`** - Loom's AI summary (needs a signed-in session)
- **`loom-pp-cli chapters [video]`** - Loom's AI chapters with timestamped deep links
- **`loom-pp-cli description [video]`** - The video's description
- **`loom-pp-cli takeaways [video]`** - Loom's AI key takeaways for a meeting recording
- **`loom-pp-cli backlinks [video]`** - Pages that link to or embed the video
- **`loom-pp-cli tags list [video]`** - Tags on a video
- **`loom-pp-cli download [video]...`** - Download MP4s by id, from a file, or in bulk (`--all`, `--since`, `--before`, `--folder`, `--match`); resumable

### Comments, action items and reactions

- **`loom-pp-cli comments list [video]`** - Comment threads with replies nested; `comments add`, `edit`, `react`, `delete` (each needs `--yes`: others see them) *(write)*
- **`loom-pp-cli tasks list [video]`** - Loom's AI action items; `tasks add`, `edit`, `approve`, `done` (each needs `--yes`: viewers see action items), `delete` (needs `--yes`) *(write)*
- **`loom-pp-cli reactions list [video]`** - Emoji reactions (live only); `reactions add` (needs `--yes`), `delete` (needs `--yes`) *(write)*

### folders, spaces, playlists, people

- **`loom-pp-cli folders list`** - Your folders, starting with the root My Videos folder
- **`loom-pp-cli folders get <folder-id>`** / **`folders search <query>`** - One folder, or find folders by name
- `folders create`, `folders rename`, `folders move`, `folders delete` *(write)*
- **`loom-pp-cli spaces list`** / **`spaces get <space-id>`** - Spaces you belong to (`--open` for all open spaces)
- **`loom-pp-cli playlists list`** / **`playlists get <playlist-id>`** - Playlists you can see, plus Watch Later
- **`loom-pp-cli recent`** - What you recently recorded and watched, and upcoming meetings
- **`loom-pp-cli users get [user-id]`** - A user's profile and video count (you, by default)

### Local store

- **`loom-pp-cli sync`** - Cache your video list, folders and spaces locally
- **`loom-pp-cli hydrate`** - Backfill transcripts, summaries, chapters and action items for synced videos
- **`loom-pp-cli search <query>`** - Full-text search over synced titles and descriptions
- **`loom-pp-cli grep [phrase]`** - Find where a phrase was spoken across hydrated transcripts
- **`loom-pp-cli digest`** - Markdown recap of a time window from the local store
- **`loom-pp-cli activity`** - New views, comments and reactions since your last check

### Setup

- **`loom-pp-cli auth login --chrome`**, **`auth status`**, **`auth logout`**, **`doctor`**

### Unavailable / limited

- **`import`** is unsupported: Loom's GraphQL API has no bulk create. Use the typed writes (`folders create`, `videos rename`, ...).
- **`download`** fails for a video Loom never rendered an MP4 for. Click Download once for that video on loom.com, then rerun; the rest of the batch still downloads.
- **`summary`** needs a signed-in session; `transcript`, `captions` and `chapters` of public videos work without one.
- **`grep` and `digest`** read only the local store: run `sync`, then `hydrate`, first.
- **`activity`**'s first run records a baseline; deltas start on the second run.
- **`--data-source`**: each read declares which sources it supports. `backlinks`, `reactions list`, `videos get`, `videos search`, `folders get`, `folders search`, `spaces get`, `playlists`, `users get`, `recent`, `activity`, `hydrate`, `download` and `videos export` read live Loom data only, so `--data-source local` exits 2; `grep` and `digest` read only the local store, so `--data-source live` exits 2. With no local store, local reads print an empty result and a hint (exit 0); a video the store does not have exits 3.
- **Multi-video runs** (`videos export`, `download`, `hydrate`, bulk `videos rename`) exit 0 when at least one video succeeded (failures are listed), with the failures' own code when every video failed, and 4 or 7 when an expired session or rate limiting stopped the run.
- **`--dry-run` on read commands** prints `would run <command>`; request bodies are shown for `sync` and writes.
- Write commands were verified by `--dry-run` preview only; no sandbox library was available.


### Self-learning loop

This CLI caches per-question discovery so repeat queries skip the walk and structurally similar queries get answered via entity substitution. The loop also self-captures: every invocation is journaled locally, and failed-flag corrections plus fresh teaches surface as candidates on the next `recall` for confirm/reject judgment. Agents call `recall` before discovery and fire `teach &` after answering. See the `## Automatic learning` section in `SKILL.md` for the full protocol.

- **`loom-pp-cli recall <query>`** - Look up cached resources for a query before running discovery
- **`loom-pp-cli teach`** - Record a query -> resource mapping (silent on success, safe to background with `&`)
- **`loom-pp-cli learnings list`** - Inspect taught rows
- **`loom-pp-cli learnings forget <query>`** - Undo a teach
- **`loom-pp-cli learnings candidates`** - List auto-captured candidates awaiting confirm/reject
- **`loom-pp-cli learnings stats`** - Local loop metrics: recall hit rate, teach-to-reuse, playbook resolution, candidate counts
- **`loom-pp-cli teach-pattern`** - Install a query/resource template up front
- **`loom-pp-cli teach-lookup`** - Add an entity mapping (e.g. country code, team alias) for pattern substitution

Pass `--no-learn` or set `LOOM_NO_LEARN=true` to disable the loop for deterministic flows.

The local store's schema version stamp is one-way: once this version of `loom-pp-cli` opens the database, older binaries refuse it with a version error — upgrade the binary rather than downgrading.

## Output Formats

```bash
# Human-readable table (default in terminal, JSON when piped)
loom-pp-cli folders list

# JSON for scripting and agents
loom-pp-cli folders list --json
# Filter to specific fields
loom-pp-cli folders list --json --select id,name

# Dry run — writes print the GraphQL operation and variables; reads print "would run <command>"
loom-pp-cli videos rename latest "New title" --dry-run

# Agent mode — JSON + compact + no prompts in one flag
loom-pp-cli folders list --agent
```

## Agent Usage

This CLI is designed for AI agent consumption:

- **Non-interactive** - never prompts, every input is a flag
- **Pipeable** - `--json` output to stdout, errors to stderr
- **Filterable** - `--select <field>[,<field>...]` returns only fields you need
- **Previewable** - `--dry-run` on writes and `sync` prints the exact GraphQL operation and variables and sends nothing; on read commands it prints `would run <command>`
- **Explicit writes** - the CLI changes your Loom library only through the write commands listed under [Commands](#commands). Every write supports `--dry-run`, which previews without `--yes`. Destructive or bulk library writes (`videos delete`, archive/unarchive, `videos move`, bulk rename, `folders delete`/`move`, `tasks delete`) and writes other people see (`videos share`, `videos settings`, `comments add`/`edit`/`react`/`delete`, `reactions add`/`delete`, `tasks add`/`edit`/`approve`/`done`) require `--yes` and exit 2 without it; they never prompt. Renaming or describing a single video (owner-only metadata) needs no `--yes`
- **Offline-friendly** - sync/search commands can use the local SQLite store when available
- **Agent-safe by default** - no colors or formatting unless `--human-friendly` is set

Exit codes: `0` success, `1` generic failure (for example the unsupported `import`), `2` usage error or a write that needs `--yes` run without it, `3` not found, `4` auth error, `5` API error, `7` rate limited, `10` config error.

## Health Check

```bash
loom-pp-cli doctor
```

Verifies configuration, credentials, and connectivity to the API.

## Configuration

Run `loom-pp-cli doctor` to see the resolved config, data, state, and cache directories. The platform-default config path is `~/.config/loom-pp-cli/config.toml`; `--home`, `LOOM_HOME`, and per-kind env vars can relocate it.

Static request headers can be configured under `headers`; per-command header overrides take precedence.

Environment variables:

| Name | Kind | Required | Description |
| --- | --- | --- | --- |
| `LOOM_COOKIE` | per_call | No (not needed after `auth login --chrome`) | Your Loom `connect.sid` session cookie, as `connect.sid=<value>` or the bare value. |

### agentcookie (optional)

If you use agentcookie to sync secrets across machines, this CLI auto-adopts agentcookie-managed credentials with no extra setup. When the daemon writes to this CLI's config, `loom-pp-cli doctor` reports `agentcookie: detected` and `auth status` labels the source as `agentcookie`. Skip this section if you don't use agentcookie - the CLI works the same as any other.

## Troubleshooting
**Authentication errors (exit code 4)**
- Run `loom-pp-cli doctor` and `loom-pp-cli auth status` to check credentials
- Re-import the session with `loom-pp-cli auth login --chrome`, or check that `LOOM_COOKIE` is set (without printing it): `test -n "$LOOM_COOKIE" && echo set`
**Not found errors (exit code 3)**
- Check the resource ID is correct
- Run `loom-pp-cli videos list` or `loom-pp-cli folders list` to see available ids

### API-specific
- **Commands report UserNotAuthorizedError or session expired** — Sign in to loom.com in Chrome, then run loom-pp-cli auth login --chrome
- **grep returns nothing for a video you know mentions the phrase** — Run loom-pp-cli hydrate --since 90d so its transcript is stored locally
- **A transcript shows as pending** — Loom is still transcribing; rerun loom-pp-cli hydrate later and it picks up pending videos

## HTTP Transport

This CLI uses Chrome-compatible HTTP transport for browser-facing endpoints. It does not require a resident browser process for normal API calls.

TLS certificates are verified by default. For a trusted development or self-signed endpoint only, pass `--insecure` for one invocation, set `LOOM_SKIP_TLS_VERIFY=true` for the current environment, or set `skip_tls_verify = true` in the config file for a persistent override.

## Discovery Signals

This CLI was generated with browser-captured traffic analysis.
- Target observed: https://www.loom.com/graphql
- Capture coverage: 274 API entries from 283 total network entries
- Reachability: standard_http (65% confidence)
- Protocols: graphql (92% confidence), rest_json (75% confidence)
- Auth: the `connect.sid` session cookie (see [Authentication](#authentication))
- Caveat: some captured requests returned empty payloads, so parts of the observed schema are inferred.

---

## Sources & Inspiration

This CLI was built by studying these projects and resources:

- [**mcp-loom**](https://github.com/karbassi/mcp-loom) — Python (9 stars)
- [**loom-transcript-mcp**](https://github.com/bStyler/loom-transcript-mcp) — TypeScript
- [**loom-remote-mcp-server-authless**](https://github.com/jackculpan/loom-remote-mcp-server-authless) — TypeScript
- [**loom-downloader**](https://github.com/EcomGraduates/loom-downloader) — JavaScript
- [**yt-dlp**](https://github.com/yt-dlp/yt-dlp) — Python

Generated by [CLI Printing Press](https://github.com/mvanhorn/cli-printing-press)
