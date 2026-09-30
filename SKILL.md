---
name: pp-loom
description: "Your Loom library as a local, searchable archive: grep what you said, back up everything, and recap a week in one command. Trigger phrases: `search my loom videos`, `find the loom where I talked about`, `get the transcript of this loom`, `recap my looms this week`, `use loom-pp-cli`."
author: "cmartinez9"
license: "Apache-2.0"
argument-hint: "<command> [args] | install cli|mcp"
allowed-tools: "Read Bash"
metadata:
  openclaw:
    requires:
      bins:
        - loom-pp-cli
---

# Loom — Printing Press CLI

## Prerequisites: Install the CLI

This skill drives the `loom-pp-cli` binary. **You must verify the CLI is installed before invoking any command from this skill.** If it is missing, install it first:

Requires Go 1.26.6 or newer.

1. Install from GitHub:
   ```bash
   go install github.com/cmartinez9/loom-cli/cmd/loom-pp-cli@latest
   ```
   Or build from a clone:
   ```bash
   git clone https://github.com/cmartinez9/loom-cli.git
   cd loom-cli
   make install
   ```
2. Verify: `loom-pp-cli --version`
3. Ensure `$GOBIN` (default `$HOME/go/bin`) is on `$PATH` for the agent/runtime that will invoke this skill.

If `--version` reports "command not found" after install, the runtime cannot see the binary directory on `$PATH`. Do not proceed with skill commands until verification succeeds.

loom-pp-cli signs in with your existing Loom browser session and syncs your library into a local SQLite store. hydrate pulls every transcript and Loom's own AI summaries and chapters, grep finds the exact moment you said something with a deep link, digest rolls a week into one recap, and activity shows new views and comments since you last looked.

## When to Use This CLI

Use this CLI when an agent needs to search, quote, summarize or back up the owner's own Loom videos, or check which recent videos got views and comments. It also makes explicit changes to the library: rename, describe, move, archive, share or delete videos, manage folders, post and edit comments, and manage Loom's action items.

Per-video reads (`transcript`, `summary`, `chapters`, `comments list` and similar) fetch live and write through to the local store, so they fall back to it when offline. `grep` and `digest` read only the local store, so questions over the whole library are fast once `sync` and `hydrate` have run.

## Anti-triggers

Do not use this CLI for:
- Recording or editing video; use the Loom app
- Anything in someone else's private workspace the signed-in user cannot open
- Transcribing non-Loom video files

## Unique Capabilities

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

## HTTP Transport

This CLI uses Chrome-compatible HTTP transport for browser-facing endpoints. It does not require a resident browser process for normal API calls.

## Discovery Signals

This CLI was generated with browser-observed traffic context.
- Capture coverage: 274 API entries from 283 total network entries
- Protocols: graphql (92% confidence), rest_json (75% confidence)
- Auth: the `connect.sid` session cookie (see Auth Setup)
- Caveat: some captured requests returned empty payloads, so parts of the observed schema are inferred.

## Command Reference

Every video argument accepts a 32-hex id, a loom.com share URL, or `latest` for your newest recording. Writes are marked **write**; see Agent Mode for the `--dry-run` / `--yes` contract.

**videos** — Videos in your Loom library

- `loom-pp-cli videos list` — List videos in your library, newest first (`--since`, `--match`, `--folder`, `--archived`, `--all`)
- `loom-pp-cli videos get <video>` — One video's metadata, pin and follow state
- `loom-pp-cli videos search <query>` — Search Loom server-side for videos, folders, spaces, tags, people and playlists
- `loom-pp-cli videos export [video]...` — Save a Markdown (or JSON) bundle per video: metadata, summary, chapters, comments and transcript (`--all`, `--since`, `--before`, `--folder`, `--match`, `--from-file`, `--out`)
- `loom-pp-cli videos rename [video] [new-name]` — **write** Rename one video, or bulk-rename by regex with `--match` / `--replace` (bulk needs `--yes`)
- `loom-pp-cli videos describe [video] [text]` — **write** Set a video's description
- `loom-pp-cli videos settings [video]` — **write** Turn downloads and comments on or off (`--downloads on|off`, `--comments on|off`; needs `--yes`)
- `loom-pp-cli videos archive [video...]` — **write** Archive videos by id, `--from-file` or filter (`--all`, `--since`, `--before`, `--folder`, `--match`; needs `--yes`)
- `loom-pp-cli videos unarchive [video...]` — **write** Restore archived videos by id or filter (needs `--yes`)
- `loom-pp-cli videos move [video...] --to <folder-id>` — **write** Move videos into a folder (needs `--yes`)
- `loom-pp-cli videos delete [video]` — **write** Move a video to Loom's trash (needs `--yes`; restore with `videos recover`)
- `loom-pp-cli videos recover [video]` — **write** Restore a deleted video from the trash
- `loom-pp-cli videos duplicate [video]` — **write** Make a copy of a video
- `loom-pp-cli videos pin [video]` — **write** Pin a video to the top of your library (`--off` to unpin)
- `loom-pp-cli videos share [video...] --space <space-id>` — **write** Share videos to one or more spaces (needs `--yes`)
- `loom-pp-cli videos follow [video]` — **write** Follow a video's comments (`--off` to stop)
- `loom-pp-cli videos watch-later [video]` — **write** Add a video to Watch Later (`--remove` to take it off)

**Per-video reads**

- `loom-pp-cli transcript [video]` — Transcript as timestamped text, text, JSON, SRT or VTT (`--format`)
- `loom-pp-cli captions [video]` — WebVTT captions exactly as Loom serves them
- `loom-pp-cli summary [video]` — Loom's AI summary (needs a signed-in session)
- `loom-pp-cli chapters [video]` — Loom's AI chapters with timestamped deep links
- `loom-pp-cli description [video]` — The video's description
- `loom-pp-cli takeaways [video]` — Loom's AI key takeaways for a meeting recording
- `loom-pp-cli backlinks [video]` — Pages that link to or embed the video (live only)
- `loom-pp-cli tags list [video]` — Tags on a video
- `loom-pp-cli download [video]...` — Download MP4s by id, from a file, or in bulk (`--all`, `--since`, `--before`, `--folder`, `--match`, `--out`); resumable and rate-limited

**comments** — `loom-pp-cli comments list [video]` lists threads with replies nested. Writes: `comments add [video] [text]` (`--at m:ss`), `comments edit [video] [comment-id] [text]`, `comments react [comment-id] [emoji]`, `comments delete [comment-id]` (all four are visible to others and need `--yes`).

**tasks** — Loom's AI action items. `loom-pp-cli tasks list [video]` lists them with deep links. Writes: `tasks add [video] [text]`, `tasks edit [task-id] [text]`, `tasks approve [task-id]`, `tasks done [task-id]` (`--undo` reopens) (all four are visible to viewers and need `--yes`), `tasks delete [task-id]` (needs `--yes`).

**reactions** — `loom-pp-cli reactions list [video]` lists emoji reactions (live only; `--summary` aggregates). Writes: `reactions add [video] [emoji]` (needs `--yes`), `reactions delete [reaction-id]` (needs `--yes`).

**folders** — Folders in your Loom library

- `loom-pp-cli folders list` — List your folders, starting with the root My Videos folder
- `loom-pp-cli folders get <folder-id>` — One folder's name, visibility, video count and parent
- `loom-pp-cli folders search <query>` — Find folders by name server-side
- `loom-pp-cli folders create [name]` — **write** Create a folder
- `loom-pp-cli folders rename [folder-id] [name]` — **write** Rename a folder
- `loom-pp-cli folders move [folder-id...] --to <folder-id>` — **write** Move folders inside another folder (needs `--yes`)
- `loom-pp-cli folders delete [folder-id...]` — **write** Delete folders (needs `--yes`)

**Spaces, playlists, people**

- `loom-pp-cli spaces list` — Spaces you belong to (`--open` for every open space)
- `loom-pp-cli spaces get <space-id>` — One space with its counts and first members
- `loom-pp-cli playlists list` — Playlists you can see, plus your Watch Later list
- `loom-pp-cli playlists get <playlist-id>` — One playlist with its videos, views and collaborators
- `loom-pp-cli recent` — What you recently recorded and watched, and your upcoming meetings
- `loom-pp-cli users get [user-id]` — A Loom user's profile and video count (you, by default)

**Local store**

- `loom-pp-cli sync` — Cache your video list, folders and spaces in the local SQLite store
- `loom-pp-cli hydrate` — Resumable backfill of transcripts, summaries, chapters and action items for synced videos
- `loom-pp-cli search <query>` — Full-text search over synced titles and descriptions (local only)
- `loom-pp-cli grep [phrase]` — Find the moment a phrase was spoken across hydrated transcripts, with deep links (local only)
- `loom-pp-cli digest` — Markdown recap of a time window's videos from Loom's summaries, chapters and action items (local only)
- `loom-pp-cli activity` — New views, comments and reactions since your last check, with a needs-reply flag

**Setup and health** — `loom-pp-cli auth login --chrome`, `loom-pp-cli auth status`, `loom-pp-cli auth logout`, `loom-pp-cli doctor`.

### Unavailable / limited

- **`import`** is unsupported: Loom's GraphQL API has no bulk create. Use the typed writes (`folders create`, `videos rename`, ...).
- **`download`** fails for a video Loom never rendered an MP4 for (it only has the streaming version). Click Download once on loom.com for that video, then rerun; other videos in the same run still download.
- **`summary`** needs a signed-in session. `transcript`, `captions` and `chapters` of public videos work without one.
- **`grep` and `digest`** read only the local store: run `sync`, then `hydrate`, first.
- **`activity`**'s first run records a baseline, so deltas start on the second run.
- **`--data-source`**: each read declares which sources it supports. Live-only reads (`backlinks`, `reactions list`, `videos get`, `videos search`, `folders get`, `folders search`, `spaces get`, `playlists`, `users get`, `recent`, `activity`, `hydrate`, `download`, `videos export`) reject `--data-source local` with exit 2; `grep` and `digest` read only the local store and reject `--data-source live`. With no local store, local reads print an empty result and a hint (exit 0); a video the store does not have exits 3.
- **Multi-video runs** (`videos export`, `download`, `hydrate`, bulk `videos rename`) exit 0 when at least one video succeeded (failures listed), with the failures' own code when every video failed, and 4 or 7 when an expired session or rate limiting stopped the run.
- **`--dry-run` on read commands** prints `would run <command>` rather than a request body. Request bodies are shown for `sync` and for writes.
- Writes were verified by `--dry-run` preview only (no sandbox library was available).

### Finding the right command

When you know what you want to do but not which command does it, ask the CLI directly:

```bash
loom-pp-cli which "<capability in your own words>"
```

`which` resolves a natural-language capability query to the best matching command from this CLI's curated feature index. Exit code `0` means at least one match; exit code `2` means no confident match — fall back to `--help` or use a narrower query. `--json` (and other machine formats) keep that exit-2 contract and write `{"matches":[]}` on stdout so agents can inspect the envelope without treating a miss as success.

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

## Auth Setup

Loom has no public API for your own videos. This CLI uses the same session cookie the Loom web app uses (connect.sid, valid about 30 days). Run auth login --chrome while signed in to loom.com in Chrome (it needs a cookie extraction tool such as pycookiecheat; doctor reports which one it found), or set LOOM_COOKIE='connect.sid=<value>' (a bare value is also accepted), then check it with auth status. When the session expires, commands say so and read-only commands fall back to the local store. Transcripts, captions and chapters of public videos work without a session; AI summaries need one.

Run `loom-pp-cli doctor` to verify setup.

## Agent Mode

Add `--agent` to any command. Expands to: `--json --compact --no-input --no-color`.

Global format flags share one contract on promoted, novel, sync, and `--deliver` paths:

- `--json` — one JSON document on stdout (sync progress events go to stderr)
- `--compact` — keep identity/status/timestamp fields; does not change the document vs stream shape
- `--csv` / `--plain` — tabular rows (collection envelopes unwrap to the row array)
- `--quiet` — one identity value per row, no envelope

- **Pipeable** — JSON on stdout, errors on stderr
- **Filterable** — `--select` keeps a subset of fields. Dotted paths descend into nested structures; arrays traverse element-wise. Critical for keeping context small on verbose APIs:

  ```bash
  loom-pp-cli folders list --agent --select id,name
  ```
- **Previewable** — `--dry-run` on writes and `sync` prints the exact GraphQL operation and variables and sends nothing; on read commands it prints `would run <command>`
- **Offline-friendly** — sync/search commands can use the local SQLite store when available
- **Non-interactive** — never prompts, every input is a flag
- **Explicit writes** — the CLI changes your Loom library only through the write commands marked **write** in the Command Reference. Every write supports `--dry-run`, which previews without `--yes`. Destructive or bulk library writes (`videos delete`, archive/unarchive, `videos move`, bulk rename, `folders delete`/`move`, `tasks delete`) and writes other people see (`videos share`, `videos settings`, `comments add`/`edit`/`react`/`delete`, `reactions add`/`delete`, `tasks add`/`edit`/`approve`/`done`) require `--yes` and exit 2 without it; they never prompt. Renaming or describing a single video needs no `--yes`. `--agent` does not imply `--yes`.

### Response envelope

Commands that read from the local store or the API wrap output in a provenance envelope:

```json
{
  "meta": {"source": "live" | "local", "synced_at": "...", "reason": "..."},
  "results": <data>
}
```

Parse `.results` for data and `.meta.source` to know whether it's live or local. A human-readable `N results (live)` summary is printed to stderr only when stdout is a terminal AND no machine-format flag (`--json`, `--csv`, `--compact`, `--quiet`, `--plain`, `--select`) is set — piped/agent consumers and explicit-format runs get pure JSON on stdout.

## Paths and state

Agents should treat the CLI's path resolver as part of the runtime contract:

- Use `--home <dir>` for one invocation, or set `LOOM_HOME=<dir>` to relocate all four path kinds under one root.
- Use per-kind env vars only when a specific kind must diverge: `LOOM_CONFIG_DIR`, `LOOM_DATA_DIR`, `LOOM_STATE_DIR`, `LOOM_CACHE_DIR`.
- Resolution order is per-kind env var, `--home`, `LOOM_HOME`, XDG (`XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_STATE_HOME`, `XDG_CACHE_HOME`), then platform defaults.
- `config` contains settings like `config.toml` and profiles. `data` contains `credentials.toml`, `data.db`, cookies, and auth sidecars. `state` contains persisted queries and `teach.log`. `cache` contains regenerable HTTP/cache files.
- Stored secrets live in `credentials.toml` under the data dir. Existing legacy `config.toml` secrets are read for compatibility and leave `config.toml` on the first auth write.
- Run `loom-pp-cli doctor --fail-on warn` to surface path and credential-location warnings. `agent-context` exposes a schema v4 `paths` block for agents that need the resolved dirs.
- For MCP, pass relocation through the MCP host config. The MCP binary does not inherit CLI flags:

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

Fleet precedence: an inherited per-kind env var overrides an explicit `--home` for that kind. Use `LOOM_HOME` or per-kind vars as durable fleet levers, and use `--home` only for a single invocation. Relocation is not reversible by unsetting env vars; move files manually before clearing `LOOM_HOME`, or `doctor` will not find credentials left under the former root.

## Automatic learning

This CLI ships a self-capturing learning loop. The CLI does its own bookkeeping: every invocation is journaled locally, a failed flag followed by a corrected retry auto-derives a `flag_alias` candidate, and a `teach` on a query family without a playbook auto-synthesizes a `playbook_candidate` from the session's journal. Your job is judgment only: `recall` first, act on surfaced candidates, `teach` the final answer, `playbook amend` when you observe a correction. You never record failures by hand.

### Step 1: `recall` before any discovery

Before list/search/drill commands on a new user question, pass the question as an argv or MCP tool argument to `recall --agent`. Do not interpolate user-controlled text into a shell command line.

Quoted `recall "<question>"` breaks on an apostrophe, which is ordinary English. A quoted heredoc breaks when a body line equals the delimiter, and that delimiter is published in these docs. Write the question with a non-shell file-writing tool, then read it back as data:

```bash
# Write the question verbatim with your file-writing tool (no shell involved).
# Command substitution on a file only ever yields data — the shell never
# parses the file's bytes as syntax.
QUERY=$(cat /path/to/question.txt)
loom-pp-cli recall "$QUERY" --agent
```

Prefer MCP: pass the question as the tool's query argument. `"$QUERY"` after a file read is argv-safe; putting the question itself in the command text is not.

The response envelope:

```json
{
  "query": "...",
  "normalized": "<normalized form>",
  "query_entities": ["..."],
  "found": true | false,
  "match_score": 0.0,
  "results": [
    { "resource_id": "...", "resource_type": "...", "venue": "...",
      "confidence": 2, "entity_match": "exact|partial|unknown",
      "source": "taught|preseed|pattern", "warnings": ["..."] }
  ],
  "mismatches": [ /* only when --debug-mismatches */ ],
  "warnings": [ /* top-level */ ],
  "candidates": [
    { "id": 12, "class": "flag_alias | playbook_candidate",
      "summary": "...", "sightings": 3, "last_seen": "...",
      "rationale": "...",
      "next_action": ["<trial command>", "loom-pp-cli learnings confirm 12"] }
  ],
  "playbook": {
    "query_family": "...",
    "playbook": {
      "steps": [ { "cmd": "<command with {slot} substitution>", "purpose": "..." } ],
      "entity_slots": ["$ENTITY"],
      "expected_tool_calls": 3
    },
    "slots_resolved": { "$ENTITY": { "token": "<live token>", "canonical": "<canonical>" } },
    "notes": "<workarounds + gotchas for this query family>"
  },
  "notes": "<duplicate surface for non-playbook callers>"
}
```

Empty-store short-circuit: if the store has no learnings, playbooks, or candidates yet (recall finds nothing and `learnings list` and `learnings candidates` are both empty), skip recall for the rest of this session instead of taxing every query; resume recall-first once something has been taught.

### Step 2: decision tree

Read `candidates`, `playbook`, `notes`, `results[0]`, and warnings in that order:

```
if Candidates present (warnings include "candidates_present"):
    -> candidates are try-then-confirm, never facts. Follow each candidate's
       two-step next_action verbatim: run the trial command first, then run
       `learnings confirm <id>` only after the trial verified the behavior.
       Reject a wrong candidate with `learnings reject <id>`.
    -> NEVER re-teach something recall surfaced as a candidate; confirm or
       reject that candidate instead of teaching a duplicate.
    -> candidates ride alongside playbooks and resource hits, not instead of
       them; continue with the branches below after acting on them.

if Playbook present:
    -> READ Playbook.notes verbatim FIRST (workarounds + gotchas the CLI surface doesn't expose)
    -> replay Playbook.steps in order, substituting Playbook.slots_resolved entries
       for the entity slot tokens. If a step's slot is unresolved, fall back to
       discovery for that step only.
    -> the Playbook's expected_tool_calls is a budget; if you find yourself running
       materially more, record the divergence via `loom-pp-cli playbook amend`
       at end-of-session.

elif Notes present (no Playbook):
    -> read Notes verbatim before any discovery step; they carry known gotchas
       for this query family even when no structured choreography exists yet.

elif Found AND Results[0].EntityMatch == "exact" AND Results[0].Confidence >= 2:
    -> skip discovery; fetch live data for Results[*].ResourceID in parallel

elif Found AND Results[0].EntityMatch == "partial":
    -> candidate hint, NOT a hit; read the resource title to validate before trusting

elif (any row in Mismatches[] when --debug-mismatches was passed):
    -> treat as cold start; the stored learning is for a different entity
       (different canonical resolved from query_entities)

else:  // Found == false, no playbook, no notes
    -> cold start; run discovery normally; teach the answer afterward (Step 4).
       If the family has no playbook yet, that teach auto-synthesizes a
       playbook candidate from this session's journal - you do not need to
       record one by hand.
```

Playbook and Notes are orthogonal to the per-resource path. A recall response can carry both a Playbook AND a `Results[]` hit - use both: the Playbook tells you which choreography to run; the resource hits short-circuit specific steps. Default to skipping `mismatches`; pass `--debug-mismatches` only when investigating cold-start surprises.

Candidate judgment details: `learnings confirm <id>` prints the candidate's full payload before materializing it - check that the printed payload matches the behavior you verified. `learnings reject <id>` tombstones the derivation signature so the same candidate does not resurface. The envelope carries only the few candidates worth acting on now; `loom-pp-cli learnings candidates` lists the full open set.

Graceful degradation: if `learnings confirm` is an unknown command, you are driving an older binary - ignore the candidates guidance and follow the rest of the protocol.

### Step 3: always read `warnings`

- `low_confidence`: row exists at `confidence<2`. Treat as a hint, not a skip-discovery hit.
- `resource_not_in_store`: the local store doesn't have the resource the learning points at. The match validator couldn't classify entities — direct-fetch and re-evaluate.
- `cross_alias_match` (per-result): the row was taught under a different alias and matched the live query's canonical via `entity_lookups` (e.g., a "USA" teach satisfying a "United States" recall). Trust the resource_id.
- `similar_shape_different_entity:<canonical>` (top-level): a structurally matching row exists but its canonical entity differs from the live query's. Treated as cold start; the warning carries the conflicting canonical as a hint, but the row is NOT promoted into Results.
- `ambiguous_alias` (top-level): a single query entity resolved to multiple canonicals (e.g., "Cards" → Arizona Cardinals + St. Louis Cardinals). Surface the ambiguity from context before committing to a resource.
- `candidates_present` (top-level): the envelope carries a `candidates` section. Handle it via the candidates branch in Step 2 before anything else.
- `lookup_refresh_available` (top-level): an entity in the query has no lookup row yet, but synced data could provide one. Run `loom-pp-cli sync` to refresh entity lookups.
- Top-level `no_learnings_for_query_family`: the table had no rows above the Jaccard floor. Pure cold start.

### Step 4: `teach &` after finalizing your response - always

Teaching is unconditional. After resolving a query the store could not answer, background-teach the final resource mapping - no call-count threshold, no judging whether it was "worth" learning. The teach is the anchor of the loop: it triggers playbook synthesis for a family without a playbook, and same-referent phrasings fold into one family so near-duplicate teaches do not fragment the store. Fire it after assembling your user-facing response but BEFORE emitting it, with a shell `&` so the call returns immediately. Pass the query the same way as recall — argv/MCP, or file-then-`$QUERY`. Do not splice the question into the command text:

```bash
QUERY=$(cat /path/to/question.txt)
loom-pp-cli teach --query "$QUERY" --resource-type <type> --resource <id1> --resource <id2>
# (append shell `&` to background it)
```

Silent on success. Errors only land in `teach.log` under the resolved state dir. Teach the **most specific** resource - if the user asked a broad question and you walked through parent records to find the specific answer, teach the leaf id, not the parent. The CLI uses seeded `entity_lookups` for cross-alias resolution at recall time, so a teach under one alias (e.g., "Niners") satisfies future queries under another alias (e.g., "49ers", "San Francisco") automatically.

PII rule: teach the structural question with identifiers stripped - never include names, emails, phone numbers, account ids, or other personal identifiers in taught queries or notes. The CLI scans teach queries for obvious email/phone shapes and warns, but does not block; strip before teaching rather than relying on the warning.

### Step 5: playbooks - optional flags, automatic synthesis

You do not need to decide whether a session "deserves" a playbook: a teach on a family without one auto-synthesizes a `playbook_candidate` from the session's journal, and the next session judges it via confirm/reject. Attach explicit playbook flags only when you already hold choreography worth recording verbatim - workarounds the CLI didn't surface (silently-dropped flags, undocumented params, pagination tricks, payload gotchas). Prefer the **integrated one-call form** - record the resource learning and the playbook in the same `teach` invocation:

```bash
# Common case: record both the resource learning AND the playbook in one call.
QUERY=$(cat /path/to/question.txt)
loom-pp-cli teach \
  --query "$QUERY" \
  --resource-type <type> \
  --resource <id> \
  --playbook-file ~/playbooks/<shape>.json \
  --playbook-notes-file ~/playbooks/<shape>-notes.md
# (append shell `&` to background it)

# Alternate: playbook-only (no resource to record alongside).
QUERY=$(cat /path/to/question.txt)
loom-pp-cli teach-playbook \
  --query "$QUERY" \
  --playbook-file ~/playbooks/<shape>.json \
  --notes-file ~/playbooks/<shape>-notes.md
```

Playbook files are JSON with `steps`, `entity_slots`, `expected_tool_calls`. Notes files are markdown carrying the gotchas verbatim. File-free callers (MCP-only agents) pass the same content inline: `--playbook-json` and `--playbook-notes` on the integrated `teach` form, `--playbook-json` and `--notes` on `teach-playbook`. On the integrated `teach` form, the playbook flags are optional - omit them entirely for a resource-only teach. On the standalone `teach-playbook` form, at least one of the playbook and notes flags must be set; both empty is rejected. Playbooks are keyed on the structural query family (entities stripped) so a recipe taught from one entity-shaped query applies to every other query of the same shape, with `slots_resolved` binding the live query's canonical at recall time.

When you DO find a playbook on a future recall, treat it as ground truth: replay the steps with `slots_resolved` substitutions, skip the discovery that the choreography already documents, and read `notes` before any step.

### Step 6: `playbook amend &` when your debug response identifies a correction

If your debug-protocol response identifies a concrete correction the notes or playbook should know — a workaround, an undocumented endpoint shape, a stale field name, observed schema drift, an empty-payload fallback — fire `playbook amend` BEFORE emitting your user-facing response. Same fire-and-forget posture as `teach`. Pass the query and note as argv/MCP arguments, or write each with a non-shell file tool and read them back (`QUERY=$(cat ...)`, `NOTE=$(cat ...)`). Do not interpolate either string into the command text:

```bash
QUERY=$(cat /path/to/question.txt)
NOTE=$(cat /path/to/note.txt)
loom-pp-cli playbook amend \
  --query "$QUERY" \
  --add-note "$NOTE"
# (append shell `&` to background it)
```

What counts as worth amending: a behavior you OBSERVED this session that future-you would benefit from knowing. Examples worth amending:

- A workaround for a CLI surface that silently drops or misorders a flag.
- An undocumented endpoint shape (response wrapped in `{meta, results}`, payload nested two levels deeper than the docs claim).
- Observed schema drift (a field renamed, an index that shifted between seasons, a category label that the API now returns lower-cased).

What does NOT belong in notes:

- The year-specific or entity-specific answer to the user's question. That's the response, not a learning.
- Per-team / per-athlete / per-row data the playbook already retrieves at runtime.
- Statements that paraphrase what the existing notes already say.

The amend command appends to the family's existing notes with a timestamped marker (`[amend YYYY-MM-DDTHH:MMZ]: <text>`). Multiple amends accumulate; the audit trail is visible. If no playbook exists yet for the family, amend creates a notes-only one (so cold-start corrections still land).

#### PII discipline for amend notes

`playbook amend` notes are designed to potentially flow upstream as shared knowledge in future versions of the Printing Press. Keep them clean of user-identifying content so the upstream-contribution path stays open without retroactive scrubbing:

- **Do NOT embed** paths to user filesystems, personal API keys or tokens, user email addresses, user GitHub handles, or specific query histories tied to a single user.
- **Acceptable**: endpoint shapes, undocumented field names, API gotchas, observed schema drift, workarounds for CLI surfaces, generalizable pagination or retry tactics.

If a correction is only meaningful with user-specific context, it belongs in a personal note, not in the playbook amend.

### Measuring the loop

`loom-pp-cli learnings stats` reports recall hit rate, teach-to-reuse, playbook resolution rate, and candidate confirm/reject counts from the local `learn_events` table. Rates are null until they have a denominator; everything stays on this machine. Use it to check whether the loop is earning its keep for this CLI.

### Disabling learning

- `--no-learn` on a single command short-circuits both `recall` and the `teach` write path. Use for deterministic agent flows or tests that must not be affected by accumulated learnings.
- `LOOM_NO_LEARN=true` in the environment globally disables the pipeline.

## Agent Feedback

When you (or the agent) notice something off about this CLI, record it:

```
loom-pp-cli feedback "the --since flag is inclusive but docs say exclusive"
loom-pp-cli feedback --stdin < notes.txt
loom-pp-cli feedback list --json --limit 10
```

Entries are stored locally as `feedback.jsonl` under the resolved data dir. They are never POSTed unless `LOOM_FEEDBACK_ENDPOINT` is set AND either `--send` is passed or `LOOM_FEEDBACK_AUTO_SEND=true`. Default behavior is local-only.

Write what *surprised* you, not a bug report. Short, specific, one line: that is the part that compounds.

## Output Delivery

Every command accepts `--deliver <sink>`. The output goes to the named sink in addition to (or instead of) stdout, so agents can route command results without hand-piping. Three sinks are supported:

| Sink | Effect |
|------|--------|
| `stdout` | Default; write to stdout only |
| `file:<path>` | Atomically write output to `<path>` (tmp + rename). Binary-response commands write decoded payload bytes (not the base64 JSON envelope) and print a small JSON receipt on stdout; `--json`/`--csv` do not refuse when this sink is set. |
| `webhook:<url>` | POST the output body to the URL (`application/json`) |

Unknown schemes are refused with a structured error naming the supported set. Webhook failures return non-zero and log the URL + HTTP status on stderr.

## Named Profiles

A profile is a saved set of flag values, reused across invocations. Use it when a scheduled or recurring agent reuses the same saved flags while providing different input each run.

```
loom-pp-cli profile save briefing --json
loom-pp-cli --profile briefing folders list
loom-pp-cli profile list --json
loom-pp-cli profile show briefing
loom-pp-cli profile delete briefing --yes
```

Explicit flags always win over profile values; profile values win over defaults. `agent-context` lists all available profiles under `available_profiles` so introspecting agents discover them at runtime.

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | Generic failure (for example an unsupported command such as `import`) |
| 2 | Usage error (wrong arguments, or a write that needs `--yes` run without it) |
| 3 | Resource not found |
| 4 | Authentication required |
| 5 | API error (upstream issue) |
| 7 | Rate limited (wait and retry) |
| 10 | Config error |

## Argument Parsing

Parse `$ARGUMENTS`:

1. **Empty, `help`, or `--help`** → show `loom-pp-cli --help` output
2. **Starts with `install`** → ends with `mcp` → MCP installation; otherwise → see Prerequisites above
3. **Anything else** → Direct Use (execute as CLI command with `--agent`)

## MCP Server Installation

1. Install the MCP server (requires Go 1.26.6 or newer):
   ```bash
   go install github.com/cmartinez9/loom-cli/cmd/loom-pp-mcp@latest
   ```
   Or run `make install-mcp` from a clone of this repository.
2. Register with Claude Code:
   ```bash
   claude mcp add loom-pp-mcp -- loom-pp-mcp
   ```
3. Verify: `claude mcp list`

## Direct Use

1. Check if installed: `which loom-pp-cli`
   If not found, offer to install (see Prerequisites at the top of this skill).
2. Match the user query to the best command from the Unique Capabilities and Command Reference above.
3. Execute with the `--agent` flag:
   ```bash
   loom-pp-cli transcript latest --agent
   loom-pp-cli videos list --since 7d --agent
   ```
4. If ambiguous, drill into subcommand help, for example `loom-pp-cli videos rename --help`. For a write, preview first: `loom-pp-cli videos delete latest --dry-run --agent`.
