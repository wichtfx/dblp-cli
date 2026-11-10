// Command dblp-cli is an offline search engine over the dblp.org computer-science
// bibliography. It downloads the full XML dump, indexes it locally with SQLite
// full-text search, and exports selected records to BibTeX or the clipboard.
//
// Usage:
//
//	dblp-cli                       interactive search (default)
//	dblp-cli update [--force]      download the latest dump and (re)index
//	dblp-cli index [path]          (re)build the index from a local dump
//	dblp-cli search <query>        one-shot search, print numbered results
//	dblp-cli bibtex <key>... [-c]  print/copy BibTeX for dblp keys
//	dblp-cli status                show dump freshness and check for updates
package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/wichtfx/dblp-cli/internal/app"
)

func main() {
	if len(os.Args) < 2 {
		// No subcommand: show freshness banner and drop into the REPL.
		app.PrintBanner()
		fail(app.Interactive())
		return
	}

	cmd, args := os.Args[1], os.Args[2:]
	switch cmd {
	case "update", "fetch", "refresh":
		fs := flag.NewFlagSet(cmd, flag.ExitOnError)
		force := fs.Bool("force", false, "re-download even if the dump is current")
		fs.Parse(args)
		fail(app.Update(*force))

	case "index":
		fs := flag.NewFlagSet(cmd, flag.ExitOnError)
		fs.Parse(args)
		fail(app.Index(fs.Arg(0)))

	case "search", "s":
		// Hand-rolled parsing so -n/-i may appear anywhere among the query
		// terms (Go's flag package stops at the first positional argument).
		limit, interactive := 20, false
		var terms []string
		for i := 0; i < len(args); i++ {
			switch args[i] {
			case "-n", "--n":
				if i+1 < len(args) {
					if v, err := strconv.Atoi(args[i+1]); err == nil && v > 0 {
						limit = v
						i++
					}
				}
			case "-i", "--interactive":
				interactive = true
			default:
				terms = append(terms, args[i])
			}
		}
		app.PrintBanner()
		if interactive || len(terms) == 0 {
			fail(app.Interactive())
			return
		}
		fail(app.Search(strings.Join(terms, " "), limit))

	case "bibtex", "bib", "cite":
		// Parse manually so -c/-o may appear anywhere among the keys.
		toClip, outFile := false, ""
		var keys []string
		for i := 0; i < len(args); i++ {
			switch args[i] {
			case "-c", "--clip":
				toClip = true
			case "-o", "--out":
				if i+1 < len(args) {
					outFile = args[i+1]
					i++
				}
			default:
				keys = append(keys, args[i])
			}
		}
		if len(keys) == 0 {
			fmt.Fprintln(os.Stderr, "usage: dblp-cli bibtex <key>... [-c] [-o file.bib]")
			os.Exit(2)
		}
		fail(app.Bibtex(keys, outFile, toClip))

	case "status", "info":
		fail(app.Status())

	case "mcp":
		fail(app.MCP())

	case "help", "-h", "--help":
		usage()

	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", cmd)
		usage()
		os.Exit(2)
	}
}

func fail(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `dblp-cli — offline search over the dblp.org bibliography

Commands:
  update [--force]     download the latest dump and rebuild the index
  index [path]         rebuild the index from a local dump
  search <query>       search and print numbered results (-n N, -i)
  bibtex <key>...      print BibTeX (-c copy to clipboard, -o write to file)
  status               show dump date/freshness and check for updates
  mcp                  serve read-only LLM tools over MCP stdio
  (no command)         interactive search

Data is stored under $XDG_DATA_HOME/dblp-cli (default ~/.local/share/dblp-cli).`)
}
