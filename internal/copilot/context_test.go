package copilot

import (
	"errors"
	"strings"
	"testing"
)

// fakeProvider is a stub SchemaProvider for context tests.
type fakeProvider struct {
	provider string
	tables   map[string][]string
	columns  map[string][][]string
	tableErr error
}

func (f *fakeProvider) GetProvider() string { return f.provider }

func (f *fakeProvider) GetTables(_ string) (map[string][]string, error) {
	if f.tableErr != nil {
		return nil, f.tableErr
	}
	return f.tables, nil
}

func (f *fakeProvider) GetTableColumns(_, table string) ([][]string, error) {
	if cols, ok := f.columns[table]; ok {
		return cols, nil
	}
	return nil, errors.New("no columns")
}

func newFakeProvider() *fakeProvider {
	return &fakeProvider{
		provider: "mysql",
		tables:   map[string][]string{"mydb": {"users"}},
		columns: map[string][][]string{
			"users": {
				{"Field", "Type"}, // header row
				{"id", "int"},
				{"email", "varchar"},
			},
		},
	}
}

func TestBuildSchemaContext(t *testing.T) {
	got := BuildSchemaContext(newFakeProvider(), "mydb")
	if !strings.Contains(got, "users(id, email)") {
		t.Fatalf("expected schema with columns, got: %q", got)
	}
	if !strings.Contains(got, "Provider: mysql") {
		t.Fatalf("expected provider line, got: %q", got)
	}
}

func TestBuildSchemaContextNilProvider(t *testing.T) {
	if got := BuildSchemaContext(nil, "mydb"); got != "" {
		t.Fatalf("expected empty context for nil provider, got: %q", got)
	}
	if got := BuildSchemaContext(newFakeProvider(), ""); got != "" {
		t.Fatalf("expected empty context for empty database, got: %q", got)
	}
}

func TestBuildContextMessageRowDataDisabled(t *testing.T) {
	in := ContextInput{
		Provider:     "mysql",
		Database:     "mydb",
		AllowRowData: false,
		Results: &ResultSet{
			Columns: []string{"id", "email"},
			Rows:    [][]string{{"1", "a@b.com"}},
		},
	}
	got := BuildContextMessage(newFakeProvider(), in)
	if strings.Contains(got, "a@b.com") {
		t.Fatalf("row data must not be included when AllowRowData is false, got: %q", got)
	}
	if !strings.Contains(got, "columns only") {
		t.Fatalf("expected columns-only note, got: %q", got)
	}
}

func TestBuildContextMessageRowDataEnabledCapsRows(t *testing.T) {
	rows := make([][]string, 0, 5)
	for i := 0; i < 5; i++ {
		rows = append(rows, []string{"v"})
	}
	in := ContextInput{
		Database:     "mydb",
		AllowRowData: true,
		MaxRows:      2,
		Results: &ResultSet{
			Columns: []string{"c"},
			Rows:    rows,
		},
	}
	got := BuildContextMessage(newFakeProvider(), in)
	if !strings.Contains(got, "truncated to 2 rows") {
		t.Fatalf("expected truncation note, got: %q", got)
	}
}

func TestBuildContextMessageIncludesQuery(t *testing.T) {
	in := ContextInput{Database: "mydb", Query: "SELECT 1"}
	got := BuildContextMessage(newFakeProvider(), in)
	if !strings.Contains(got, "SELECT 1") {
		t.Fatalf("expected query in context, got: %q", got)
	}
}

func TestExtractSQLBlocks(t *testing.T) {
	md := "Here is a query:\n```sql\nSELECT * FROM users;\n```\nand prose\n```\nDELETE FROM x;\n```\n```python\nprint(1)\n```"
	blocks := ExtractSQLBlocks(md)
	if len(blocks) != 2 {
		t.Fatalf("expected 2 SQL blocks, got %d: %v", len(blocks), blocks)
	}
	if blocks[0] != "SELECT * FROM users;" {
		t.Fatalf("unexpected first block: %q", blocks[0])
	}
	if blocks[1] != "DELETE FROM x;" {
		t.Fatalf("expected untagged SQL-looking block, got: %q", blocks[1])
	}
}

func TestExtractSQLBlocksIgnoresNonSQL(t *testing.T) {
	md := "```\njust some text\n```"
	if blocks := ExtractSQLBlocks(md); len(blocks) != 0 {
		t.Fatalf("expected no SQL blocks, got: %v", blocks)
	}
}

func TestIsModifying(t *testing.T) {
	if IsModifying("SELECT * FROM t") {
		t.Fatal("SELECT should not be modifying")
	}
	if !IsModifying("DELETE FROM t") {
		t.Fatal("DELETE should be modifying")
	}
}
