// Package store is the SQLite-backed search index over dblp records.
//
// Layout:
//
//	pubs  - one row per record; `data` holds the record as JSON, without the
//	        fields that have columns of their own, so we
//	        can regenerate BibTeX without re-parsing the dump.
//	fts   - FTS5 virtual table (external-content over pubs) providing
//	        full-text search with BM25 ranking over title/authors/venue/key.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/wichtfx/dblp-cli/internal/dump"

	_ "modernc.org/sqlite" // pure-Go driver, registers "sqlite"
)

// Store wraps the database handle.
type Store struct{ db *sql.DB }

const schema = `
CREATE TABLE IF NOT EXISTS pubs (
  id      INTEGER PRIMARY KEY,
  key     TEXT UNIQUE,
  type    TEXT,
  title   TEXT,
  authors TEXT,
  venue   TEXT,
  year    TEXT,
  data    TEXT          -- record as JSON, without the fields that have columns
);
CREATE VIRTUAL TABLE IF NOT EXISTS fts USING fts5(
  title, authors, venue, key,
  content='pubs', content_rowid='id',
  tokenize='unicode61 remove_diacritics 2'
);
CREATE TABLE IF NOT EXISTS author_pubs (
  author TEXT NOT NULL,
  pub_id INTEGER NOT NULL,
  PRIMARY KEY (author, pub_id)
) WITHOUT ROWID;
`

// Open opens (creating if needed) the index at path.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // SQLite writer is single-threaded
	// Wait up to 5s for a lock rather than failing immediately, so a read that
	// races the tail of an index build succeeds instead of erroring.
	if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// Result is a search hit shown in listings.
type Result struct {
	Key     string
	Type    string
	Title   string
	Authors string
	Venue   string
	Year    string
}

// Builder performs a bulk (re)build of the index inside one transaction with
// durability pragmas relaxed for speed — acceptable since the source of truth
// is the dump on disk and we rebuild from scratch.
type Builder struct {
	s    *Store
	tx   *sql.Tx
	ins  *sql.Stmt
	auth []link // author identity -> publication
	dois []link // DOI -> publication
	rowN int64
}

type link struct {
	value string
	pubID int64
}

const linkBatchSize = 1000

// PreparedRecord contains the CPU-side representation used by the SQLite
// writer. Preparing records is independent of the database and can therefore
// run concurrently with XML parsing and insertion.
type PreparedRecord struct {
	Key        string
	Type       string
	Title      string
	Authors    string
	RawAuthors []string
	DOIs       []string
	Venue      string
	Year       string
	Data       string
}

func PrepareRecord(r dump.Record) (PreparedRecord, error) {
	// The stored JSON leaves out the fields that have columns of their own;
	// Get restores them from the columns.
	stored := r
	stored.Type, stored.Key, stored.Title, stored.Year = "", "", "", ""
	blob, err := json.Marshal(stored)
	if err != nil {
		return PreparedRecord{}, err
	}
	return PreparedRecord{
		Key:        r.Key,
		Type:       r.Type,
		Title:      r.Title,
		Authors:    strings.Join(r.CleanAuthors(), ", "),
		RawAuthors: r.Authors,
		DOIs:       r.DOIs(),
		Venue:      r.Venue(),
		Year:       r.Year,
		Data:       string(blob),
	}, nil
}

// loadSchema replaces the tables for a bulk build. The unique index on key,
// the author table and the DOI table are built by Commit after the load:
// building an index from all rows at once is several times faster than
// updating it per row.
const loadSchema = `
DROP TABLE IF EXISTS fts;
DROP TABLE IF EXISTS author_pubs;
DROP TABLE IF EXISTS author_links;
DROP TABLE IF EXISTS doi_pubs;
DROP TABLE IF EXISTS doi_links;
DROP TABLE IF EXISTS pubs;
CREATE TABLE pubs (
  id      INTEGER PRIMARY KEY,
  key     TEXT,
  type    TEXT,
  title   TEXT,
  authors TEXT,
  venue   TEXT,
  year    TEXT,
  data    TEXT
);
CREATE VIRTUAL TABLE fts USING fts5(
  title, authors, venue, key,
  content='pubs', content_rowid='id',
  tokenize='unicode61 remove_diacritics 2'
);
CREATE TABLE author_links (author TEXT NOT NULL, pub_id INTEGER NOT NULL);
CREATE TABLE doi_links (doi TEXT NOT NULL, pub_id INTEGER NOT NULL);
`

// NewBuilder resets the index and returns a Builder ready for Add calls.
func (s *Store) NewBuilder() (*Builder, error) {
	for _, p := range []string{
		"PRAGMA journal_mode=OFF",
		"PRAGMA synchronous=OFF",
		"PRAGMA temp_store=MEMORY",
		"PRAGMA locking_mode=EXCLUSIVE",
		"PRAGMA cache_size=-2097152", // up to 2 GiB during the bulk build
		loadSchema,
	} {
		if _, err := s.db.Exec(p); err != nil {
			return nil, err
		}
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	ins, err := tx.Prepare(`INSERT INTO pubs(id,key,type,title,authors,venue,year,data)
		VALUES(?,?,?,?,?,?,?,?)`)
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	return &Builder{s: s, tx: tx, ins: ins,
		auth: make([]link, 0, linkBatchSize), dois: make([]link, 0, linkBatchSize)}, nil
}

// Add inserts one record into the index.
func (b *Builder) Add(r dump.Record) error {
	prepared, err := PrepareRecord(r)
	if err != nil {
		return err
	}
	return b.AddPrepared(prepared)
}

// AddPrepared inserts a record prepared by PrepareRecord. Calls must remain
// serial because Builder owns a single SQLite transaction.
func (b *Builder) AddPrepared(r PreparedRecord) error {
	b.rowN++
	if _, err := b.ins.Exec(b.rowN, r.Key, r.Type, r.Title, r.Authors, r.Venue, r.Year, r.Data); err != nil {
		return err
	}
	for _, author := range r.RawAuthors {
		b.auth = append(b.auth, link{value: author, pubID: b.rowN})
	}
	for _, doi := range r.DOIs {
		b.dois = append(b.dois, link{value: doi, pubID: b.rowN})
	}
	if len(b.auth) >= linkBatchSize {
		if err := b.flush("author_links", &b.auth); err != nil {
			return err
		}
	}
	if len(b.dois) >= linkBatchSize {
		return b.flush("doi_links", &b.dois)
	}
	return nil
}

// flush inserts the buffered links into table and empties the buffer.
func (b *Builder) flush(table string, links *[]link) error {
	if len(*links) == 0 {
		return nil
	}
	var query strings.Builder
	query.WriteString("INSERT INTO " + table + " VALUES ")
	args := make([]any, 0, len(*links)*2)
	for i, l := range *links {
		if i > 0 {
			query.WriteByte(',')
		}
		query.WriteString("(?,?)")
		args = append(args, l.value, l.pubID)
	}
	if _, err := b.tx.Exec(query.String(), args...); err != nil {
		return err
	}
	*links = (*links)[:0]
	return nil
}

// Count returns how many records have been added so far; after Commit, how
// many remain once duplicate keys are removed.
func (b *Builder) Count() int64 { return b.rowN }

// Commit builds the unique key index, the author table and the DOI table
// from the loaded rows, builds FTS in one bulk pass, and then optimises it.
// Building each index after the load is considerably cheaper than updating
// it once for every publication.
func (b *Builder) Commit() error {
	if err := b.flush("author_links", &b.auth); err != nil {
		return err
	}
	if err := b.flush("doi_links", &b.dois); err != nil {
		return err
	}
	b.ins.Close()
	deduped, err := b.createKeyIndex()
	if err != nil {
		return err
	}
	where := ""
	if deduped {
		where = ` WHERE pub_id IN (SELECT id FROM pubs)`
	}
	for _, q := range []string{
		`CREATE TABLE author_pubs (
		  author TEXT NOT NULL,
		  pub_id INTEGER NOT NULL,
		  PRIMARY KEY (author, pub_id)
		) WITHOUT ROWID`,
		`INSERT OR IGNORE INTO author_pubs SELECT author, pub_id FROM author_links` + where + ` ORDER BY author, pub_id`,
		`DROP TABLE author_links`,
		`CREATE TABLE doi_pubs (
		  doi    TEXT NOT NULL,
		  pub_id INTEGER NOT NULL,
		  PRIMARY KEY (doi, pub_id)
		) WITHOUT ROWID`,
		`INSERT OR IGNORE INTO doi_pubs SELECT doi, pub_id FROM doi_links` + where + ` ORDER BY doi, pub_id`,
		`DROP TABLE doi_links`,
	} {
		if _, err := b.tx.Exec(q); err != nil {
			return err
		}
	}
	if err := b.tx.Commit(); err != nil {
		return err
	}
	if _, err := b.s.db.Exec("INSERT INTO fts(fts) VALUES('rebuild')"); err != nil {
		return err
	}
	_, err = b.s.db.Exec("INSERT INTO fts(fts) VALUES('optimize')")
	return err
}

// createKeyIndex builds the unique index on key. The dump occasionally
// repeats a key; the later copies are deleted first, keeping the first, and
// deduped reports whether any was. The duplicates must be gone before the
// index is created: with the journal off, a failed CREATE UNIQUE INDEX
// cannot be rolled back and leaves the schema corrupt.
func (b *Builder) createKeyIndex() (deduped bool, err error) {
	res, err := b.tx.Exec(`DELETE FROM pubs WHERE id IN (
		SELECT id FROM (SELECT id, row_number() OVER (PARTITION BY key ORDER BY id) AS n FROM pubs)
		WHERE n > 1)`)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	b.rowN -= n
	_, err = b.tx.Exec(`CREATE UNIQUE INDEX pubs_key ON pubs(key)`)
	return n > 0, err
}

// SearchAuthor returns publications belonging to one exact DBLP author
// identity. rawAuthor may include DBLP's numeric homonym suffix.
func (s *Store) SearchAuthor(ctx context.Context, rawAuthor, year string, limit, offset int) ([]Result, error) {
	var indexed bool
	if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM author_pubs LIMIT 1)`).Scan(&indexed); err != nil {
		return nil, err
	}
	if !indexed {
		// Compatibility with indexes created before the author mapping existed.
		// A rebuild restores exact homonym-aware matching.
		return s.SearchQuery(ctx, ColumnPhrase("authors", dump.CleanName(rawAuthor)), year, limit, offset)
	}

	q := `SELECT p.key, p.type, p.title, p.authors, p.venue, p.year
		FROM author_pubs a JOIN pubs p ON p.id = a.pub_id
		WHERE a.author = ? AND p.key NOT LIKE 'homepages/%'`
	args := []any{rawAuthor}
	if year != "" {
		q += " AND p.year = ?"
		args = append(args, year)
	}
	q += " ORDER BY p.year DESC, p.title LIMIT ? OFFSET ?"
	args = append(args, limit, offset)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanResults(rows)
}

// SearchVenue returns publications whose venue is exactly venue, newest
// first. The FTS phrase narrows the candidates; the equality test removes
// venues that only contain the phrase, such as workshops of a conference.
func (s *Store) SearchVenue(ctx context.Context, venue, year string, limit, offset int) ([]Result, error) {
	q := `SELECT p.key, p.type, p.title, p.authors, p.venue, p.year
		FROM fts JOIN pubs p ON p.id = fts.rowid
		WHERE fts MATCH ? AND p.venue = ?`
	args := []any{ColumnPhrase("venue", venue), venue}
	if year != "" {
		q += " AND p.year = ?"
		args = append(args, year)
	}
	q += " ORDER BY p.year DESC, p.title LIMIT ? OFFSET ?"
	args = append(args, limit, offset)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanResults(rows)
}

// ErrNoDOIIndex reports an index built before DOI lookup existed.
var ErrNoDOIIndex = errors.New("this index has no DOI table; rebuild it with `dblp-cli index`")

// ByDOI returns the records whose links carry doi, which may be written as a
// bare DOI, a doi.org URL or with a "doi:" prefix.
func (s *Store) ByDOI(ctx context.Context, doi string) ([]dump.Record, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT p.key FROM doi_pubs d JOIN pubs p ON p.id = d.pub_id
		WHERE d.doi = ? ORDER BY p.id`, dump.NormalizeDOI(doi))
	if err != nil {
		if strings.Contains(err.Error(), "no such table") {
			return nil, ErrNoDOIIndex
		}
		return nil, err
	}
	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			rows.Close()
			return nil, err
		}
		keys = append(keys, key)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var recs []dump.Record
	for _, key := range keys {
		r, ok, err := s.Get(ctx, key)
		if err != nil {
			return nil, err
		}
		if ok {
			recs = append(recs, r)
		}
	}
	return recs, nil
}

// Search runs a free-text query and returns up to limit results ranked by
// BM25, with title matches weighted highest.
func (s *Store) Search(ctx context.Context, query string, limit, offset int) ([]Result, error) {
	return s.search(ctx, buildMatch(query), "", query, limit, offset)
}

// SearchQuery runs an already-built FTS5 MATCH expression, optionally
// restricted to a single year. It backs the interactive facet drill-down
// (filter by an author or venue, narrowed to a year).
func (s *Store) SearchQuery(ctx context.Context, match, year string, limit, offset int) ([]Result, error) {
	return s.search(ctx, match, year, match, limit, offset)
}

// search is the shared query path. display is the user-facing query string
// used only to phrase syntax errors.
func (s *Store) search(ctx context.Context, match, year, display string, limit, offset int) ([]Result, error) {
	if match == "" {
		return nil, nil
	}
	q := `SELECT p.key, p.type, p.title, p.authors, p.venue, p.year
		FROM fts JOIN pubs p ON p.id = fts.rowid
		WHERE fts MATCH ?`
	args := []any{match}
	if year != "" { // year isn't an FTS column, so filter the matched rows
		q += " AND p.year = ?"
		args = append(args, year)
	}
	q += " ORDER BY bm25(fts, 10.0, 5.0, 2.0, 1.0) LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		if strings.Contains(err.Error(), "fts5: syntax error") {
			return nil, fmt.Errorf("invalid query %q — searchable columns are "+
				"title:, authors:, venue:, key: and you must put AND between grouped "+
				"clauses, e.g. title:(bert OR gpt) AND authors:devlin", display)
		}
		return nil, err
	}
	defer rows.Close()
	return scanResults(rows)
}

func scanResults(rows *sql.Rows) ([]Result, error) {
	var out []Result
	for rows.Next() {
		var r Result
		if err := rows.Scan(&r.Key, &r.Type, &r.Title, &r.Authors, &r.Venue, &r.Year); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ColumnPhrase builds an FTS5 column-scoped phrase term, e.g.
// ColumnPhrase("authors", `Jacob Devlin`) -> authors:"Jacob Devlin".
// Embedded quotes are neutralised so the phrase can't break the query.
func ColumnPhrase(col, val string) string {
	val = strings.TrimSpace(strings.ReplaceAll(val, `"`, " "))
	return fmt.Sprintf(`%s:"%s"`, col, val)
}

// Get returns the full record for a dblp key, ok=false if not found.
func (s *Store) Get(ctx context.Context, key string) (dump.Record, bool, error) {
	var typ, title, year, blob string
	err := s.db.QueryRowContext(ctx, `SELECT type, title, year, data FROM pubs WHERE key = ?`, key).Scan(&typ, &title, &year, &blob)
	if err == sql.ErrNoRows {
		return dump.Record{}, false, nil
	}
	if err != nil {
		return dump.Record{}, false, err
	}
	var r dump.Record
	if err := json.Unmarshal([]byte(blob), &r); err != nil {
		return dump.Record{}, false, err
	}
	r.Type, r.Key, r.Title, r.Year = typ, key, title, year
	return r, true, nil
}

// Count returns the number of indexed records.
func (s *Store) Count() (int64, error) {
	var n int64
	err := s.db.QueryRow(`SELECT count(*) FROM pubs`).Scan(&n)
	return n, err
}

// buildMatch turns a free-text query into an FTS5 MATCH expression. Bare
// words become prefix terms ANDed together, except words of one or two
// letters, which are matched exactly: as prefixes they match most of the
// index and make the search take seconds; anything containing FTS5 operator
// characters is passed through so power users can write their own queries.
const shortTerm = 2

func buildMatch(q string) string {
	q = strings.TrimSpace(q)
	if q == "" {
		return ""
	}
	if strings.ContainsAny(q, `":*()^`) || strings.Contains(q, " OR ") {
		return q
	}
	fields := strings.Fields(q)
	terms := make([]string, 0, len(fields))
	for _, f := range fields {
		f = strings.ReplaceAll(f, `"`, "")
		if utf8.RuneCountInString(f) <= shortTerm {
			terms = append(terms, fmt.Sprintf(`"%s"`, f))
		} else {
			terms = append(terms, fmt.Sprintf(`"%s"*`, f))
		}
	}
	return strings.Join(terms, " ")
}
