package corpus

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/limbic-systems/stargate/internal/config"
)

func testCorpusConfig(path string) config.CorpusConfig {
	return config.CorpusConfig{
		Enabled:       true,
		Path:          path,
		MaxPrecedents: 5,
		MinSimilarity: 0.7,
		MaxAge:        "90d",
		MaxEntries:    10000,
		PruneInterval: "1h",
		MaxWritesPerMinute:      10,
		MaxReasoningLength:      1000,
		StoreDecisions:          "all",
		StoreReasoning:          true,
		StoreRawCommand:         true,
		StoreUserApprovals:      true,
		MaxPrecedentsPerDecision: 3,
	}
}

func TestOpenCreatesDBAndTables(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	cfg := testCorpusConfig(dbPath)

	c, err := Open(t.Context(), cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer c.Close()

	// Verify the file exists.
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Fatal("database file was not created")
	}

	// Verify the table exists by querying it.
	var count int
	err = c.DB().QueryRow("SELECT COUNT(*) FROM precedents").Scan(&count)
	if err != nil {
		t.Fatalf("query precedents table: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 rows, got %d", count)
	}
}

func TestOpenWALMode(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	cfg := testCorpusConfig(dbPath)

	c, err := Open(t.Context(), cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer c.Close()

	var mode string
	err = c.DB().QueryRow("PRAGMA journal_mode").Scan(&mode)
	if err != nil {
		t.Fatalf("query journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Errorf("journal_mode = %q, want wal", mode)
	}
}

func TestOpenFilePermissions(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	cfg := testCorpusConfig(dbPath)

	c, err := Open(t.Context(), cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer c.Close()

	info, err := os.Stat(dbPath)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	perm := info.Mode().Perm()
	if perm&0077 != 0 {
		t.Errorf("permissions = %o, want no group/other access", perm)
	}
}

func TestOpenCreatesParentDirectory(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "subdir", "nested", "test.db")
	cfg := testCorpusConfig(dbPath)

	c, err := Open(t.Context(), cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer c.Close()

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Fatal("database file was not created in nested directory")
	}
}

func TestSchemaHasExpectedIndexes(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	cfg := testCorpusConfig(dbPath)

	c, err := Open(t.Context(), cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer c.Close()

	expectedIndexes := []string{
		"idx_precedents_hash",
		"idx_precedents_created",
		"idx_precedents_decision",
		"idx_precedents_trace",
		"idx_precedents_trace_decision",
	}

	for _, idx := range expectedIndexes {
		var name string
		err := c.DB().QueryRow(
			"SELECT name FROM sqlite_master WHERE type='index' AND name=?", idx,
		).Scan(&name)
		if err != nil {
			t.Errorf("index %q not found: %v", idx, err)
		}
	}
}

func TestCloseOrdering(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	cfg := testCorpusConfig(dbPath)

	c, err := Open(t.Context(), cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// Insert a row to ensure WAL has data.
	_, err = c.DB().Exec(`
		INSERT INTO precedents (signature, signature_hash, command_names, flags, decision)
		VALUES ('[]', 'abc123', '["test"]', '[]', 'allow')
	`)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	// Close should not hang or panic.
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// DB should be closed — queries should fail.
	var count int
	err = c.DB().QueryRow("SELECT COUNT(*) FROM precedents").Scan(&count)
	if err == nil {
		t.Error("expected error querying closed database")
	}
}

func TestOpenEmptyPathReturnsError(t *testing.T) {
	cfg := testCorpusConfig("")
	_, err := Open(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestSchemaHasExpectedColumns(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	cfg := testCorpusConfig(dbPath)

	c, err := Open(t.Context(), cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer c.Close()

	expectedColumns := []string{
		"id", "signature", "signature_hash", "raw_command", "command_names",
		"flags", "ast_summary", "cwd", "decision", "reasoning", "risk_factors",
		"matched_rule", "scopes_in_play", "stargate_trace_id", "created_at",
		"last_hit_at", "hit_count", "session_id", "agent",
	}

	rows, err := c.DB().Query("PRAGMA table_info(precedents)")
	if err != nil {
		t.Fatalf("table_info: %v", err)
	}
	defer rows.Close()

	columns := make(map[string]bool)
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull int
		var dfltValue *string
		var pk int
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
			t.Fatalf("scan: %v", err)
		}
		columns[name] = true
	}

	for _, col := range expectedColumns {
		if !columns[col] {
			t.Errorf("missing column: %s", col)
		}
	}
}
