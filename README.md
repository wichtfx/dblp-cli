# dblp — offline search over the dblp.org bibliography

A single Go binary that downloads the full [dblp.org](https://dblp.org) computer-science
bibliography dump, indexes it locally with SQLite full-text search, and lets you
search it offline and export selected records to **BibTeX** or the **clipboard**.

Unlike the existing `dblp-cli` tools that query the live dblp.org API, this works
entirely against a local dump — fast, offline, and rate-limit free — while still
telling you how old your dump is and how to refresh it.

## Motivation

dblp is an invaluable research tool, but the website is down often enough that
depending on it during a literature search can be frustrating. Network access is
not guaranteed either: reading and writing on a train, for example, is much easier
when the bibliography remains searchable through tunnels and patchy mobile
coverage. `dblp-cli` keeps a complete local copy for precisely those situations.

The same source is useful to language models. LLMs can invent plausible-looking
titles, authors, venues, and citation keys; the MCP server lets them check those
claims against the local dblp index and produce BibTeX from an actual record. This
does not eliminate hallucinations, but it gives an LLM a reliable bibliographic
anchor instead of asking it to recall citations from its training data.

## Install

```sh
go build -o dblp .      # produces a self-contained binary (no cgo)
```

## Quick start

```sh
dblp update              # download the latest dump (~1 GB gz) and build the index
dblp search dijkstra goto statement
dblp bibtex journals/cacm/Dijkstra68          # print BibTeX
dblp bibtex journals/cacm/Dijkstra68 -c       # copy to clipboard
dblp                     # interactive search (default with no arguments)
```

## Commands

| Command                 | Description                                                                                                                                  |
| ----------------------- | -------------------------------------------------------------------------------------------------------------------------------------------- |
| `dblp update [--force]` | Download the latest dump from dblp.org and rebuild the index. Skips the download if you already have the current dump (`--force` overrides). |
| `dblp index [path]`     | Rebuild the index from a local dump (defaults to the downloaded one).                                                                        |
| `dblp search <query>`   | Search and print numbered results. `-n N` sets the result count, `-i` opens interactive mode.                                                |
| `dblp bibtex <key>...`  | Print BibTeX for one or more dblp keys. `-c` copies to the clipboard, `-o file.bib` writes to a file.                                        |
| `dblp status`           | Show the dump date, freshness, record count, and check dblp.org for a newer dump.                                                            |
| `dblp mcp`              | Start a read-only MCP stdio server for LLM clients.                                                                                           |
| `dblp` (no args)        | Interactive search REPL.                                                                                                                     |

### Interactive mode

Results are color-coded (number, **title**, _venue_, _year_, and publication type).
DBLP keys and the startup command guide use the terminal's normal text intensity
for readability. Color is disabled automatically when output is piped, or when
`NO_COLOR` is set.

```
dblp> deep learning attention      # search
  1. Attention Is All You Need.
     Ashish Vaswani, ... — NIPS (2017)
     inproceedings · conf/nips/VaswaniSPUJGKP17
  ...
dblp> 1            # select result 1 → shows its authors, venue and year
dblp> 2            # browse all work by the selected paper's 2nd author
dblp> au 1         # re-search by its 1st author (Ashish Vaswani)
dblp> au 1 2020    # ...restricted to a year (use `y` for this paper's year)
dblp> ve           # re-search by its venue (NIPS); `ve y` adds the year
dblp> b 1 3        # print BibTeX for results 1 and 3
dblp> c 1          # copy result 1 to the clipboard
dblp> w refs.bib all   # write the whole page to a file
dblp> o 1          # show the dblp page link
dblp> n / p        # next / previous page
dblp> q            # quit
```

The Up and Down arrow keys browse command history. Left and Right browse the
previous and next publication pages without entering `p` or `n` explicitly.

Selecting an author homepage opens that author's publications directly. The
index keeps DBLP's internal author identity, so authors with the same displayed
name are not mixed together. Use `n` and `p` to browse the complete publication
list. If the index was built by a version of `dblp-cli` without author-homepage
support, run `dblp index` once to add the exact author mapping.

**Drill-down / faceting.** Type a result's number to *select* it; the tool then lists
its authors (numbered), venue and year. From there `au <k>` pivots the search to that
author, or simply type the author's displayed number. `ve` pivots to the venue. Both
commands can be narrowed by a trailing year (`y` means "this paper's year").
Drill-downs page with `n`/`p` like any search.

## Searching

- **Free text** is matched as prefix terms across title, authors, venue and key:
  `dblp search vaswani attention` finds papers by author _and_ title word.
- Search is **diacritic-insensitive** (`schmidhuber` matches _Schmidhüber_).

### Field-scoped keyword queries

dblp records carry no abstracts or subject keywords, so the searchable columns are
**`title:`**, **`authors:`**, **`venue:`** and **`key:`**. Any query containing an
FTS5 operator (`:` `"` `*` `()` `OR`) is passed straight to
[FTS5](https://www.sqlite.org/fts5.html#full_text_query_syntax):

```sh
dblp search 'title:transformer authors:vaswani'           # column keywords
dblp search 'title:"attention is all you need"'           # exact phrase
dblp search 'title:diffusion NOT venue:CoRR'              # exclusion
dblp search 'title:(bert OR gpt) AND authors:devlin'      # grouping (note the AND)
```

> Put an explicit **`AND`** between clauses when one of them is a parenthesised
> group — FTS5 rejects an implicit AND directly after `(...)`.

## How freshness works

dblp publishes a new dump roughly daily. Every command prints a one-line banner
with the dump's date and age, and warns when it is more than 30 days old:

```
Dump: 2026-06-22 (1 day(s) old) · 8.30M records
```

`dblp status` additionally issues a lightweight `HEAD` request to dblp.org to tell
you whether a newer dump exists, and `dblp update` only re-downloads when the
server's dump is actually newer than yours. Downloads show a progress bar and
automatically fall back to the official Dagstuhl and Trier mirrors when the
canonical server is unavailable. Indexing has its own record-count progress bar.

## Data location

Everything lives under `$XDG_DATA_HOME/dblp-cli` (default `~/.local/share/dblp-cli`):

- `dblp.xml.gz` — the compressed dump
- `dblp.db` — the SQLite full-text index
- `meta.json` — dump date, download/index timestamps, record count

## LLM access with MCP

After building the index, configure an MCP-compatible LLM client to launch the
binary with the `mcp` command. For example:

```json
{
  "mcpServers": {
    "dblp": {
      "command": "/absolute/path/to/dblp",
      "args": ["mcp"]
    }
  }
}
```

The server exposes five read-only tools: `search_publications`,
`get_publication`, `get_bibtex`, `browse_author_publications`, and
`browse_venue_publications`. Search and browse results include `hasMore`, `limit`,
and `offset` pagination metadata. An LLM can search first, inspect an exact
record, browse its author or venue, and only then request BibTeX. The server
communicates using newline-delimited JSON-RPC on stdin/stdout; it does not expose
arbitrary SQL or write operations.

## Design notes

- **Streaming parse.** The dump is multi-GB; it is parsed token-by-token
  (`encoding/xml`) so memory stays flat regardless of dump size.
- **No external DTD needed.** dblp.xml uses HTML named entities (`&uuml;`) and an
  ISO-8859-1 declaration; both are handled in-process (`xml.HTMLEntity` plus a
  small Latin-1 charset reader), so the private `dblp.dtd` is never required.
- **Pure-Go SQLite** (`modernc.org/sqlite`) with FTS5 keeps the build cgo-free and
  the result a single portable binary.
- **Bulk FTS construction.** Publications are imported first, then FTS5 builds
  its search index in one pass instead of maintaining it for every inserted row.
- **Batched author indexing.** Author-to-publication mappings are inserted in
  batches while parsing, avoiding several SQLite calls per publication without
  retaining the dump in memory.
- **dblp homonym suffixes** (e.g. _Wei Wang 0001_) are stripped for clean display
  and BibTeX output.
