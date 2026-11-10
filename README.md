# dblp-cli: offline search over the dblp.org bibliography

dblp-cli is a single Go binary. It downloads the complete [dblp.org](https://dblp.org) bibliography dump, indexes it locally with SQLite full-text search, searches the index offline, and writes selected records as BibTeX to the terminal, a file or the clipboard.

Other tools named `dblp-cli` query the dblp.org API. This one queries a local copy of the dump, so a search needs no network connection and is subject to no rate limit. Every command prints the date of the local dump and how to update it.

## Motivation

The dblp.org website is often unavailable, and a network connection is not always available either, for example on a train. dblp-cli keeps a complete local copy of the bibliography for both cases.

A language model can invent titles, authors, venues and citation keys. The MCP server of dblp-cli lets a model check a citation against the local index and take its BibTeX from a dblp record instead of from its training data. The model can still invent a citation; it can now also check it.

## Install

```sh
go build -o dblp-cli .   # a self-contained binary, without cgo
```

## Quick start

```sh
dblp-cli update                                  # download the latest dump (about 1 GB, compressed) and build the index
dblp-cli search dijkstra goto statement
dblp-cli bibtex journals/cacm/Dijkstra68         # print BibTeX
dblp-cli bibtex journals/cacm/Dijkstra68 -c      # copy BibTeX to the clipboard
dblp-cli                                         # interactive search, the default with no arguments
```

## Commands

| Command                     | Description                                                                                                                                          |
| --------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------- |
| `dblp-cli update [--force]` | Download the latest dump from dblp.org and rebuild the index. The download is skipped when the local dump is current; `--force` downloads it anyway. |
| `dblp-cli index [path]`     | Rebuild the index from a local dump, by default the downloaded one.                                                                                  |
| `dblp-cli search <query>`   | Search and print numbered results. `-n N` sets the number of results; `-i` opens interactive mode.                                                   |
| `dblp-cli bibtex <key>...`  | Print BibTeX for one or more dblp keys. `-c` copies it to the clipboard; `-o file.bib` writes it to a file.                                          |
| `dblp-cli status`           | Print the dump date, its age and the number of records, and check dblp.org for a newer dump.                                                         |
| `dblp-cli mcp`              | Start a read-only MCP server on standard input and output, for LLM clients.                                                                          |
| `dblp-cli` (no arguments)   | Interactive search.                                                                                                                                  |

### Interactive mode

Each result is printed in colour: its number, title, venue, year and publication type. dblp keys and the command guide at startup are printed in the terminal's normal text intensity. Colour is off when the output is piped or `NO_COLOR` is set.

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
dblp> w refs.bib all   # append the whole page to a file
dblp> o 1          # show the dblp page link
dblp> n / p        # next / previous page
dblp> q            # quit
```

The Up and Down arrow keys browse the command history. The Left and Right arrow keys show the previous and next page of results, as `p` and `n` do.

Typing the number of a result selects it, and the selected result's authors, numbered, its venue and its year are printed. `au <k>` or the number `k` searches for the publications of the selection's `k`-th author, and `au` without `k` for those of its first author. `ve` searches for the publications of the selection's venue. Both take a year as a further argument, and `y` stands for the selection's year. `n` and `p` page through these results as through any search.

Selecting an author homepage lists that author's publications. The index stores dblp's identity of each author, so two authors with the same name are kept apart. An index built by a version of dblp-cli without this feature does not have that identity; `dblp-cli index` rebuilds it with it.

## Searching

A free-text query matches each term as a prefix against the title, the authors, the venue and the key: `dblp-cli search vaswani attention` finds the papers with author Vaswani and the title word "attention".

Search ignores diacritics: `schmidhuber` matches _Schmidhüber_.

### Queries on a single field

dblp records have no abstracts and no subject keywords. The fields that can be searched are `title:`, `authors:`, `venue:` and `key:`. A query that contains an FTS5 operator, `:` `"` `*` `()` or `OR`, is passed to [FTS5](https://www.sqlite.org/fts5.html#full_text_query_syntax) unchanged:

```sh
dblp-cli search 'title:transformer authors:vaswani' # terms in given fields
dblp-cli search 'title:"attention is all you need"' # exact phrase
dblp-cli search 'title:diffusion NOT venue:CoRR' # exclusion
dblp-cli search 'title:(bert OR gpt) AND authors:devlin' # grouping, with an explicit AND
```

A clause that follows a parenthesised group must be joined to it with `AND`: FTS5 rejects an implicit AND after `(...)`.

## The age of the dump

dblp publishes a new dump about once a day. Every command prints the date and the age of the local dump, and a warning when it is more than 30 days old:

```
Dump: 2026-06-22 (1 day(s) old) · 8.30M records
```

`dblp-cli status` sends a `HEAD` request to dblp.org to check whether a newer dump exists. `dblp-cli update` downloads the dump only when the one on the server is newer than the local one. The download prints a progress bar; when dblp.org does not respond, it is taken from the official mirrors at Dagstuhl and Trier. Indexing prints a progress bar of the number of records.

## Where the data is kept

All data is kept in `$XDG_DATA_HOME/dblp-cli`, by default `~/.local/share/dblp-cli`:

- `dblp.xml.gz`, the compressed dump;
- `dblp.db`, the SQLite full-text index;
- `meta.json`, the dump date, the times of the last download and the last indexing, and the number of records.

## Access for language models over MCP

With the [Model Context Protocol (MCP)](https://modelcontextprotocol.io/), a language model calls tools provided by a local process. The MCP server of dblp-cli provides a model with the same offline index as the command line. A model uses it to check that a citation exists, to find the correct author or venue, and to take BibTeX from a stored record instead of from its training data.

The server requires an index: run `dblp-cli update` first, or `dblp-cli index` for a dump already downloaded. The MCP client then starts the binary with the `mcp` command. A typical configuration is:

```json
{
  "mcpServers": {
    "dblp": {
      "command": "/absolute/path/to/dblp-cli",
      "args": ["mcp"]
    }
  }
}
```

`command` must be an absolute path, since an MCP client does not necessarily have the `PATH` of an interactive shell. The name and location of the configuration file depend on the client.

### MCP tools

| Tool                         | Purpose                                                                                                        | Arguments                                       |
| ---------------------------- | -------------------------------------------------------------------------------------------------------------- | ----------------------------------------------- |
| `search_publications`        | Search the index, by full text or by field, and return a ranked summary of each publication with its dblp key. | `query`, optional `limit` and `offset`          |
| `get_publication`            | Return the complete stored dblp record for one key.                                                            | `key`                                           |
| `get_bibtex`                 | Return BibTeX for up to 100 dblp keys in one call.                                                             | `keys`                                          |
| `browse_author_publications` | List the publications of one dblp author identity, without the publications of authors with the same name.     | `author`, optional `year`, `limit` and `offset` |
| `browse_venue_publications`  | List the publications of one journal, conference or book title.                                                | `venue`, optional `year`, `limit` and `offset`  |

`search_publications` takes the same field queries as the command line, for example `title:(bert OR gpt) AND authors:devlin`. A search or browse response contains a summary of each publication and the fields `hasMore`, `limit` and `offset`. When `hasMore` is true, the next page is requested with `offset` increased by `limit`.

`browse_author_publications` takes the author's name as `get_publication` returns it. dblp appends a number to a name shared by several people, as in `Wei Wang 0001`. The command line hides that number; the MCP tools keep it, so that each name identifies one person.

### Checking a citation

1. Call `search_publications` with title words, an author or a field query.
2. Choose a result by its title, year, venue and dblp key.
3. Call `get_publication` to check the complete record.
4. If needed, list the publications of the author or the venue.
5. Call `get_bibtex` for the checked keys only.

Typical requests to an LLM client are "Find the original BERT paper and give me its DBLP BibTeX" and "List the 2024 publications in this paper's venue". The client chooses when to call a tool; the bibliographic data comes from dblp-cli.

The server implements MCP version 2025-06-18, with newline-delimited JSON-RPC on standard input and output. Every tool is marked read-only, non-destructive, idempotent and closed-world. The tools read the local index and nothing else: they run no SQL given by the client, write nothing to the database, download nothing, and neither use the clipboard nor write files. Once the index is built, the server needs no network connection. Standard output carries protocol messages only, and the server writes its diagnostics to standard error.

## Design

- **The dump is parsed as a stream.** The dump is several gigabytes; `encoding/xml` reads it token by token, and memory use does not grow with its size.
- **The dblp DTD is not needed.** `dblp.xml` uses HTML named entities, such as `&uuml;`, and declares the ISO-8859-1 encoding. dblp-cli decodes both itself, with `xml.HTMLEntity` and a small Latin-1 reader, so the file `dblp.dtd` is never read.
- **SQLite is written in Go.** `modernc.org/sqlite`, with FTS5, builds without cgo, and the result is a single portable binary.
- **The full-text index is built in one pass.** The publications are imported first, and FTS5 then builds its index once, instead of updating it after each row.
- **Authors are indexed in batches.** The mapping from authors to publications is inserted in batches during parsing, which saves several SQLite calls per publication without holding the dump in memory.
- **Records are prepared concurrently.** A bounded pool of workers encodes the JSON and cleans the metadata while the parser reads and the SQLite writer writes.
- **The numbers dblp appends to shared names**, as in _Wei Wang 0001_, are removed from the output on the terminal and in BibTeX.
