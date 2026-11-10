// Package mcp exposes the local dblp index through the Model Context Protocol.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/wichtfx/dblp-cli/internal/bibtex"
	"github.com/wichtfx/dblp-cli/internal/store"
)

const protocolVersion = "2025-06-18"

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type tool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"inputSchema"`
	Annotations any    `json:"annotations,omitempty"`
}

var toolList = []tool{
	{
		Name:        "search_publications",
		Description: "Search the local DBLP bibliography index. Returns ranked publication summaries and DBLP keys.",
		InputSchema: schema(map[string]any{
			"query":  map[string]any{"type": "string", "description": "Words or an FTS5 query using title:, authors:, venue:, or key:"},
			"limit":  map[string]any{"type": "integer", "minimum": 1, "maximum": 100, "default": 20},
			"offset": map[string]any{"type": "integer", "minimum": 0, "default": 0},
		}, []string{"query"}),
		Annotations: readOnlyAnnotations(),
	},
	{
		Name:        "browse_author_publications",
		Description: "Browse publications by one exact DBLP author identity. Use the raw author value returned by get_publication to preserve homonym disambiguation.",
		InputSchema: pagedSchema(map[string]any{
			"author": map[string]any{"type": "string", "description": "Exact DBLP author identity, including a numeric suffix when present"},
			"year":   map[string]any{"type": "string", "description": "Optional publication year"},
		}, []string{"author"}),
		Annotations: readOnlyAnnotations(),
	},
	{
		Name:        "browse_venue_publications",
		Description: "Browse publications from an exact journal, conference, or book-title venue.",
		InputSchema: pagedSchema(map[string]any{
			"venue": map[string]any{"type": "string", "description": "Exact venue name returned by search_publications or get_publication"},
			"year":  map[string]any{"type": "string", "description": "Optional publication year"},
		}, []string{"venue"}),
		Annotations: readOnlyAnnotations(),
	},
	{
		Name:        "get_publication",
		Description: "Get complete bibliographic metadata for one exact DBLP key.",
		InputSchema: schema(map[string]any{
			"key": map[string]any{"type": "string", "description": "DBLP key, for example journals/cacm/Dijkstra68"},
		}, []string{"key"}),
		Annotations: readOnlyAnnotations(),
	},
	{
		Name:        "get_bibtex",
		Description: "Generate BibTeX for one or more exact DBLP keys.",
		InputSchema: schema(map[string]any{
			"keys": map[string]any{
				"type": "array", "items": map[string]any{"type": "string"},
				"minItems": 1, "maxItems": 100,
			},
		}, []string{"keys"}),
		Annotations: readOnlyAnnotations(),
	},
}

func readOnlyAnnotations() map[string]any {
	return map[string]any{
		"readOnlyHint":    true,
		"destructiveHint": false,
		"idempotentHint":  true,
		"openWorldHint":   false,
	}
}

func schema(properties map[string]any, required []string) map[string]any {
	return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
}

func pagedSchema(properties map[string]any, required []string) map[string]any {
	properties["limit"] = map[string]any{"type": "integer", "minimum": 1, "maximum": 100, "default": 20}
	properties["offset"] = map[string]any{"type": "integer", "minimum": 0, "default": 0}
	return schema(properties, required)
}

// Serve reads newline-delimited JSON-RPC messages from in and writes MCP
// responses to out. Stdio is the MCP transport; diagnostics must go elsewhere.
func Serve(ctx context.Context, st *store.Store, in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	enc := json.NewEncoder(out)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		var req request
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			if err := enc.Encode(response{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{-32700, "parse error"}}); err != nil {
				return err
			}
			continue
		}
		// Notifications have no id and never receive a response.
		if len(req.ID) == 0 {
			continue
		}
		resp := dispatch(st, req)
		if err := enc.Encode(resp); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func dispatch(st *store.Store, req request) response {
	r := response{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "initialize":
		r.Result = map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]any{"name": "dblp-cli", "version": "1.0.0"},
		}
	case "ping":
		r.Result = map[string]any{}
	case "tools/list":
		r.Result = map[string]any{"tools": toolList}
	case "tools/call":
		result, err := callTool(st, req.Params)
		var unknown unknownToolError
		if errors.As(err, &unknown) {
			// The specification treats an unknown tool as a protocol error.
			r.Error = &rpcError{-32602, unknown.Error()}
		} else if err != nil {
			r.Result = map[string]any{"content": []any{map[string]any{"type": "text", "text": err.Error()}}, "isError": true}
		} else {
			r.Result = result
		}
	default:
		r.Error = &rpcError{-32601, "method not found"}
	}
	return r
}

func callTool(st *store.Store, raw json.RawMessage) (any, error) {
	var p struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("invalid tool arguments: %w", err)
	}
	var value any
	switch p.Name {
	case "search_publications":
		q, err := stringArg(p.Arguments, "query")
		if err != nil {
			return nil, err
		}
		limit, err := intArg(p.Arguments, "limit", 20, 0, 100)
		if err != nil {
			return nil, err
		}
		offset, err := intArg(p.Arguments, "offset", 0, -1, 1_000_000_000)
		if err != nil {
			return nil, err
		}
		results, err := st.Search(q, limit+1, offset)
		if err != nil {
			return nil, err
		}
		value = publicationPage(results, limit, offset)
	case "browse_author_publications":
		author, err := stringArg(p.Arguments, "author")
		if err != nil {
			return nil, err
		}
		year, err := optionalStringArg(p.Arguments, "year")
		if err != nil {
			return nil, err
		}
		limit, err := intArg(p.Arguments, "limit", 20, 0, 100)
		if err != nil {
			return nil, err
		}
		offset, err := intArg(p.Arguments, "offset", 0, -1, 1_000_000_000)
		if err != nil {
			return nil, err
		}
		results, err := st.SearchAuthor(author, year, limit+1, offset)
		if err != nil {
			return nil, err
		}
		value = publicationPage(results, limit, offset)
	case "browse_venue_publications":
		venue, err := stringArg(p.Arguments, "venue")
		if err != nil {
			return nil, err
		}
		year, err := optionalStringArg(p.Arguments, "year")
		if err != nil {
			return nil, err
		}
		limit, err := intArg(p.Arguments, "limit", 20, 0, 100)
		if err != nil {
			return nil, err
		}
		offset, err := intArg(p.Arguments, "offset", 0, -1, 1_000_000_000)
		if err != nil {
			return nil, err
		}
		results, err := st.SearchVenue(venue, year, limit+1, offset)
		if err != nil {
			return nil, err
		}
		value = publicationPage(results, limit, offset)
	case "get_publication":
		key, err := stringArg(p.Arguments, "key")
		if err != nil {
			return nil, err
		}
		record, ok, err := st.Get(key)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("publication not found: %s", key)
		}
		value = record
	case "get_bibtex":
		keys, err := stringSliceArg(p.Arguments, "keys")
		if err != nil {
			return nil, err
		}
		var rendered []string
		for _, key := range keys {
			record, ok, err := st.Get(key)
			if err != nil {
				return nil, err
			}
			if !ok {
				return nil, fmt.Errorf("publication not found: %s", key)
			}
			rendered = append(rendered, bibtex.Render(record))
		}
		value = map[string]any{"bibtex": strings.Join(rendered, "\n")}
	default:
		return nil, unknownToolError(p.Name)
	}
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"content":           []any{map[string]any{"type": "text", "text": string(b)}},
		"structuredContent": value,
	}, nil
}

// unknownToolError names a tool the server does not provide.
type unknownToolError string

func (e unknownToolError) Error() string { return "unknown tool: " + string(e) }

func publicationPage(results []store.Result, limit, offset int) map[string]any {
	hasMore := len(results) > limit
	if hasMore {
		results = results[:limit]
	}
	return map[string]any{
		"publications": results,
		"offset":       offset,
		"limit":        limit,
		"hasMore":      hasMore,
	}
}

func stringArg(args map[string]any, name string) (string, error) {
	v, ok := args[name].(string)
	if !ok || strings.TrimSpace(v) == "" {
		return "", fmt.Errorf("%s must be a non-empty string", name)
	}
	return v, nil
}

func optionalStringArg(args map[string]any, name string) (string, error) {
	v, exists := args[name]
	if !exists {
		return "", nil
	}
	s, ok := v.(string)
	if !ok || strings.TrimSpace(s) == "" {
		return "", fmt.Errorf("%s must be a non-empty string when provided", name)
	}
	return s, nil
}

func intArg(args map[string]any, name string, def, min, max int) (int, error) {
	v, ok := args[name]
	if !ok {
		return def, nil
	}
	n, err := strconv.Atoi(fmt.Sprint(v))
	if err != nil || n <= min || n > max {
		return 0, fmt.Errorf("%s must be between %d and %d", name, min+1, max)
	}
	return n, nil
}

func stringSliceArg(args map[string]any, name string) ([]string, error) {
	v, ok := args[name].([]any)
	if !ok || len(v) == 0 || len(v) > 100 {
		return nil, fmt.Errorf("%s must be an array of 1 to 100 strings", name)
	}
	out := make([]string, len(v))
	for i, item := range v {
		s, ok := item.(string)
		if !ok || strings.TrimSpace(s) == "" {
			return nil, fmt.Errorf("%s must contain only non-empty strings", name)
		}
		out[i] = s
	}
	return out, nil
}
