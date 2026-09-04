package copilot

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/jorgerojas26/lazysql/drivers"
)

// SchemaProvider is the subset of drivers.Driver needed to build schema context.
// Using an interface keeps this package testable without a real database.
type SchemaProvider interface {
	GetProvider() string
	GetTables(database string) (map[string][]string, error)
	GetTableColumns(database, table string) ([][]string, error)
}

// ResultSet is a tabular result to (optionally) include as context.
type ResultSet struct {
	// Columns are the header names.
	Columns []string
	// Rows are data rows (excluding the header).
	Rows [][]string
}

// ContextInput describes what to include when building a context message.
type ContextInput struct {
	Provider     string
	Database     string
	ReadOnly     bool
	Query        string     // current editor query, optional
	Results      *ResultSet // current results, optional
	AllowRowData bool       // gate for including Results rows
	MaxRows      int        // cap for included rows
}

// BuildSchemaContext returns a human-readable schema summary for the given
// database using the provider. Errors are tolerated: partial context is better
// than none.
func BuildSchemaContext(provider SchemaProvider, database string) string {
	if provider == nil || database == "" {
		return ""
	}
	tablesMap, err := provider.GetTables(database)
	if err != nil || len(tablesMap) == 0 {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Database: %s\n", database)
	fmt.Fprintf(&b, "Provider: %s\n", provider.GetProvider())
	b.WriteString("Schema:\n")

	for schema, tables := range tablesMap {
		for _, tbl := range tables {
			qualified := tbl
			if schema != "" && schema != database {
				qualified = schema + "." + tbl
			}
			cols, err := provider.GetTableColumns(database, qualified)
			if err != nil || len(cols) < 2 {
				fmt.Fprintf(&b, "- %s\n", qualified)
				continue
			}
			names := make([]string, 0, len(cols)-1)
			for i := 1; i < len(cols); i++ {
				if len(cols[i]) > 0 && cols[i][0] != "" {
					names = append(names, cols[i][0])
				}
			}
			fmt.Fprintf(&b, "- %s(%s)\n", qualified, strings.Join(names, ", "))
		}
	}
	return b.String()
}

// BuildContextMessage assembles a single context string from schema, the current
// query and (only when allowed) a bounded sample of result rows.
func BuildContextMessage(provider SchemaProvider, in ContextInput) string {
	var b strings.Builder
	b.WriteString("Current database context:\n")
	if in.Provider != "" {
		fmt.Fprintf(&b, "Provider: %s\n", in.Provider)
	}
	if in.Database != "" {
		fmt.Fprintf(&b, "Database: %s\n", in.Database)
	}
	if in.ReadOnly {
		b.WriteString("Connection mode: READ-ONLY (only SELECT queries are permitted)\n")
	}

	if schema := BuildSchemaContext(provider, in.Database); schema != "" {
		b.WriteString("\n")
		b.WriteString(schema)
	}

	if strings.TrimSpace(in.Query) != "" {
		b.WriteString("\nCurrent editor query:\n```sql\n")
		b.WriteString(strings.TrimSpace(in.Query))
		b.WriteString("\n```\n")
	}

	if in.Results != nil && len(in.Results.Columns) > 0 {
		b.WriteString("\nCurrent result set")
		if !in.AllowRowData {
			b.WriteString(" (row data sharing is disabled; columns only):\n")
			fmt.Fprintf(&b, "Columns: %s\n", strings.Join(in.Results.Columns, ", "))
		} else {
			maxRows := in.MaxRows
			if maxRows <= 0 {
				maxRows = DefaultMaxRows
			}
			rows := in.Results.Rows
			truncated := false
			if len(rows) > maxRows {
				rows = rows[:maxRows]
				truncated = true
			}
			b.WriteString(":\n")
			fmt.Fprintf(&b, "Columns: %s\n", strings.Join(in.Results.Columns, ", "))
			for _, r := range rows {
				fmt.Fprintf(&b, "%s\n", strings.Join(r, " | "))
			}
			if truncated {
				fmt.Fprintf(&b, "... (truncated to %d rows)\n", maxRows)
			}
		}
	}

	return b.String()
}

// sqlBlockRE matches fenced code blocks, capturing the language and body.
var sqlBlockRE = regexp.MustCompile("(?s)```([a-zA-Z]*)\\n(.*?)```")

// ExtractSQLBlocks returns the bodies of fenced code blocks in the markdown that
// look like SQL (either tagged as sql or containing SQL keywords).
func ExtractSQLBlocks(markdown string) []string {
	matches := sqlBlockRE.FindAllStringSubmatch(markdown, -1)
	var out []string
	for _, m := range matches {
		lang := strings.ToLower(strings.TrimSpace(m[1]))
		body := strings.TrimSpace(m[2])
		if body == "" {
			continue
		}
		if lang == "sql" || (lang == "" && looksLikeSQL(body)) {
			out = append(out, body)
		}
	}
	return out
}

var sqlKeywordRE = regexp.MustCompile(`(?i)^\s*(SELECT|WITH|INSERT|UPDATE|DELETE|CREATE|ALTER|DROP|TRUNCATE|EXPLAIN|SHOW|DESCRIBE|DESC|GRANT|REVOKE|MERGE|REPLACE)\b`)

func looksLikeSQL(s string) bool {
	return sqlKeywordRE.MatchString(s)
}

// IsModifying reports whether the SQL is a data/schema mutation. It reuses the
// driver-level classifier so behavior matches read-only enforcement elsewhere.
func IsModifying(query string) bool {
	return drivers.IsQueryMutation(query)
}
