package corpus

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/limbic-systems/stargate/internal/config"
	"github.com/limbic-systems/stargate/internal/ttlmap"
)

func boolPtr(b bool) *bool { return &b }
func intPtr(i int) *int    { return &i }

func openRawDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

func testCorpusConfig(path string) config.CorpusConfig {
	return config.CorpusConfig{
		Enabled:       boolPtr(true),
		Path:          path,
		MaxPrecedents: 5,
		MinSimilarity: 0.7,
		MaxAge:        "90d",
		MaxEntries:    intPtr(10000),
		PruneInterval: "1h",
		MaxWritesPerMinute:      10,
		MaxReasoningLength:      1000,
		StoreDecisions:          "all",
		StoreReasoning:          boolPtr(true),
		StoreRawCommand:         boolPtr(true),
		StoreUserApprovals:      boolPtr(true),
		MaxPrecedentsPerPolarity: 3,
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

func TestOpenPragmas(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	cfg := testCorpusConfig(dbPath)

	c, err := Open(t.Context(), cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer c.Close()

	tests := []struct {
		pragma string
		want   string
	}{
		{"journal_mode", "delete"},
		{"synchronous", "1"}, // NORMAL = 1
		{"temp_store", "2"},  // MEMORY = 2
	}
	for _, tt := range tests {
		var got string
		err := c.DB().QueryRow("PRAGMA " + tt.pragma).Scan(&got)
		if err != nil {
			t.Fatalf("PRAGMA %s: %v", tt.pragma, err)
		}
		if got != tt.want {
			t.Errorf("PRAGMA %s = %q, want %q", tt.pragma, got, tt.want)
		}
	}
}

func TestOpenWaitsBusyTimeout(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "busy.db")

	// Create the DB and schema so it exists for the concurrent open.
	cfg := testCorpusConfig(dbPath)
	c1, err := Open(t.Context(), cfg)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}

	// Hold an EXCLUSIVE lock — this blocks all other connections including
	// the journal_mode switch in Open() which requires an exclusive lock.
	if _, err := c1.DB().Exec("BEGIN EXCLUSIVE"); err != nil {
		t.Fatalf("begin exclusive: %v", err)
	}

	// Release the lock after a short delay in a goroutine.
	done := make(chan struct{})
	go func() {
		time.Sleep(200 * time.Millisecond)
		c1.DB().Exec("COMMIT") //nolint:errcheck
		close(done)
	}()

	// Second open must wait (busy_timeout) rather than fail immediately.
	start := time.Now()
	c2, err := Open(t.Context(), cfg)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("second Open should succeed after busy wait, got: %v", err)
	}
	c2.Close()

	if elapsed < 150*time.Millisecond {
		t.Errorf("Open returned in %v, expected >= 150ms wait for lock release", elapsed)
	}

	<-done
	c1.Close()
}

func TestOpenMigratesWALToDelete(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "migrate.db")
	walPath := dbPath + "-wal"

	// Create a WAL-mode database with autocheckpoint disabled so data
	// stays in the WAL file and is NOT checkpointed into the main DB.
	db, err := openRawDB(dbPath)
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		t.Fatalf("set WAL: %v", err)
	}
	if _, err := db.Exec("PRAGMA wal_autocheckpoint=0"); err != nil {
		t.Fatalf("disable autocheckpoint: %v", err)
	}
	if err := createSchema(db); err != nil {
		t.Fatalf("create schema: %v", err)
	}

	// Record schema-only state of the main DB file.
	schemaSnapshot, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatalf("read schema snapshot: %v", err)
	}

	if _, err := db.Exec(`INSERT INTO precedents
		(signature, signature_hash, raw_command, command_names, flags,
		 ast_summary, cwd, decision, reasoning, risk_factors,
		 matched_rule, scopes_in_play, stargate_trace_id, session_id, agent)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		`[{"name":"git","subcommand":"status","flags":[],"context":"top_level"}]`,
		"abc123", "git status", `["git"]`, `[]`, "git status", "/tmp",
		"allow", "read-only git", `[]`, "", `[]`, "", "", ""); err != nil {
		t.Fatalf("insert: %v", err)
	}

	// Capture the WAL (which holds the INSERT since autocheckpoint is off).
	walSnapshot, err := os.ReadFile(walPath)
	if err != nil {
		t.Fatalf("read WAL: %v", err)
	}
	if len(walSnapshot) == 0 {
		t.Fatal("WAL should contain data before close")
	}

	// Close checkpoints the WAL into the main file and removes it.
	db.Close()

	// Restore stranded-WAL state: schema-only main file + WAL with data.
	// This simulates the crash/disk-full scenario where WAL was never
	// checkpointed into the main DB.
	if err := os.WriteFile(dbPath, schemaSnapshot, 0600); err != nil {
		t.Fatalf("restore schema snapshot: %v", err)
	}
	if err := os.WriteFile(walPath, walSnapshot, 0600); err != nil {
		t.Fatalf("restore WAL: %v", err)
	}

	// Reopen via Open() which must recover the WAL then switch to DELETE.
	cfg := testCorpusConfig(dbPath)
	c, err := Open(t.Context(), cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer c.Close()

	// Verify journal mode migrated.
	var mode string
	if err := c.DB().QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatalf("query journal_mode: %v", err)
	}
	if mode != "delete" {
		t.Errorf("journal_mode = %q after migration, want delete", mode)
	}

	// Verify data from the stranded WAL was recovered (including JSON fields).
	entries, err := c.ExportAll()
	if err != nil {
		t.Fatalf("ExportAll: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries after stranded WAL recovery, want 1", len(entries))
	}
	e := entries[0]
	if e.RawCommand != "git status" {
		t.Errorf("RawCommand = %q, want %q", e.RawCommand, "git status")
	}
	if len(e.CommandNames) != 1 || e.CommandNames[0] != "git" {
		t.Errorf("CommandNames = %v, want [git]", e.CommandNames)
	}
	if e.Decision != "allow" {
		t.Errorf("Decision = %q, want allow", e.Decision)
	}

	// WAL file should be gone after migration to DELETE mode.
	if _, err := os.Stat(walPath); !os.IsNotExist(err) {
		t.Errorf("WAL file should not exist after DELETE migration, got err=%v", err)
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

// ---- Write / Lookup tests ----

// sampleEntry returns a PrecedentEntry suitable for testing.
func sampleEntry(sig, hash, decision string) PrecedentEntry {
	return PrecedentEntry{
		Signature:     sig,
		SignatureHash: hash,
		RawCommand:    "git status",
		CommandNames:  []string{"git"},
		Flags:         []string{},
		Decision:      decision,
		Reasoning:     "test reasoning",
		RiskFactors:   []string{"r1"},
		ScopesInPlay:  []string{"scope1"},
		TraceID:       "trace-" + hash,
		SessionID:     "sess-1",
		Agent:         "test-agent",
	}
}

// openTestCorpus opens a corpus with a fresh temp DB.
func openTestCorpus(t *testing.T) *Corpus {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	cfg := testCorpusConfig(dbPath)
	c, err := Open(t.Context(), cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

// TestWriteAndReadBack writes an entry and verifies it can be read by hash.
func TestWriteAndReadBack(t *testing.T) {
	c := openTestCorpus(t)

	sig := `[{"name":"git","subcommand":"status","flags":[],"context":"top_level"}]`
	hash := hashString(sig)
	e := sampleEntry(sig, hash, "allow")

	if err := c.Write(e); err != nil {
		t.Fatalf("Write: %v", err)
	}

	var count int
	if err := c.db.QueryRow("SELECT COUNT(*) FROM precedents WHERE signature_hash = ?", hash).Scan(&count); err != nil {
		t.Fatalf("query: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 row, got %d", count)
	}
}

// TestWriteDuplicateSignatureRateLimited verifies the per-signature rate limit
// blocks a second write for the same hash within an hour.
func TestWriteDuplicateSignatureRateLimited(t *testing.T) {
	c := openTestCorpus(t)

	sig := `[{"name":"curl","subcommand":"","flags":["-s"],"context":"top_level"}]`
	hash := hashString(sig)
	e := sampleEntry(sig, hash, "allow")

	if err := c.Write(e); err != nil {
		t.Fatalf("first Write: %v", err)
	}

	err := c.Write(e)
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("expected ErrRateLimited, got %v", err)
	}
}

// TestWriteAfterRateLimitExpires verifies that a write succeeds once the TTL
// has elapsed by replacing the rate limiter with a very short TTL map.
func TestWriteAfterRateLimitExpires(t *testing.T) {
	c := openTestCorpus(t)

	// Replace the sig rate limiter with one that has a 50ms TTL sweep.
	c.sigRateLimit = ttlmap.New[string, struct{}](t.Context(), ttlmap.Options{
		SweepInterval: 10 * time.Millisecond,
	})

	sig := `[{"name":"ls","subcommand":"","flags":[],"context":"top_level"}]`
	hash := hashString(sig)
	e := sampleEntry(sig, hash, "allow")

	// First write — set a very short TTL so it expires quickly.
	if err := c.Write(e); err != nil {
		t.Fatalf("first Write: %v", err)
	}

	// Overwrite the rate limit entry with a 50ms TTL so it expires.
	// Key format is signature_hash:decision (matches write.go).
	c.sigRateLimit.Set(hash+":"+e.Decision, struct{}{}, 50*time.Millisecond)

	time.Sleep(100 * time.Millisecond) // wait for expiry

	// Second write — should succeed now.
	if err := c.Write(e); err != nil {
		t.Fatalf("second Write after expiry: %v", err)
	}
}

// TestGlobalRateLimit verifies that exceeding max_writes_per_minute returns
// ErrRateLimited.
func TestGlobalRateLimit(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	cfg := testCorpusConfig(dbPath)
	cfg.MaxWritesPerMinute = 3

	c, err := Open(t.Context(), cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer c.Close()

	// Write 3 entries with distinct signatures — all should succeed.
	for i := 0; i < 3; i++ {
		sig := fmt.Sprintf(`[{"name":"grep%d","subcommand":"","flags":[],"context":"top_level"}]`, i)
		e := sampleEntry(sig, "", "allow") // hash computed by Write
		if err := c.Write(e); err != nil {
			t.Fatalf("Write %d: %v", i, err)
		}
	}

	// 4th write should hit the global limit.
	sig := `[{"name":"grep","subcommand":"","flags":[],"context":"top_level"}]`
	e := sampleEntry(sig, "", "allow")
	err = c.Write(e)
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("expected ErrRateLimited on global limit, got %v", err)
	}
}

// --- LookupSimilar tests ---

// writeSig is a helper that writes an entry with a given signature, bypassing
// the per-signature rate limit after the first write by resetting the TTL entry.
func writeSig(t *testing.T, c *Corpus, sig, decision string) {
	t.Helper()
	hash := hashString(sig)
	e := sampleEntry(sig, hash, decision)
	// Remove any existing rate limit entry so we can write multiple (key is hash:decision).
	c.sigRateLimit.Delete(hash + ":" + decision)
	if err := c.Write(e); err != nil {
		t.Fatalf("writeSig(%q, %q): %v", decision, sig, err)
	}
}

func gitSig() string {
	return `[{"name":"git","subcommand":"status","flags":[],"context":"top_level"}]`
}

func curlSig() string {
	return `[{"name":"curl","subcommand":"","flags":["-s"],"context":"top_level"}]`
}

func defaultLookupConfig(c *Corpus) LookupConfig {
	return LookupConfig{
		MinSimilarity:  0.0,
		MaxPrecedents:  20,
		MaxPerPolarity: 10,
		MaxAge:         24 * time.Hour,
	}
}

// TestLookupSimilarFindsEntries writes 3 entries with the same signature and
// verifies LookupSimilar returns them.
func TestLookupSimilarFindsEntries(t *testing.T) {
	c := openTestCorpus(t)
	sig := gitSig()

	// Write 3 allow entries with the same signature; clear the per-sig rate
	// limit between writes so each one is accepted.
	for i := 0; i < 3; i++ {
		writeSig(t, c, sig, "allow")
	}

	results, err := c.LookupSimilar([]string{"git"}, sig, defaultLookupConfig(c))
	if err != nil {
		t.Fatalf("LookupSimilar: %v", err)
	}
	if len(results) != 3 {
		t.Errorf("expected 3 results, got %d", len(results))
	}
}

// TestLookupSimilarPolarityBalance writes 3 allow and 3 deny entries and
// verifies MaxPerPolarity=3 caps each side at 3.
func TestLookupSimilarPolarityBalance(t *testing.T) {
	c := openTestCorpus(t)
	sig := gitSig()

	// Write 3 allow and 3 deny entries; clear the per-sig rate limit between
	// writes (keyed on hash:decision) so each one is accepted.
	for i := 0; i < 3; i++ {
		writeSig(t, c, sig, "allow")
	}
	for i := 0; i < 3; i++ {
		writeSig(t, c, sig, "deny")
	}

	cfg := LookupConfig{
		MinSimilarity:  0.0,
		MaxPrecedents:  20,
		MaxPerPolarity: 3,
		MaxAge:         24 * time.Hour,
	}
	results, err := c.LookupSimilar([]string{"git"}, sig, cfg)
	if err != nil {
		t.Fatalf("LookupSimilar: %v", err)
	}

	allowCount, denyCount := 0, 0
	for _, r := range results {
		switch r.Decision {
		case "allow", "user_approved":
			allowCount++
		case "deny":
			denyCount++
		}
	}
	if allowCount > 3 {
		t.Errorf("allow count %d exceeds MaxPerPolarity 3", allowCount)
	}
	if denyCount > 3 {
		t.Errorf("deny count %d exceeds MaxPerPolarity 3", denyCount)
	}
}

// TestLookupSimilarUserApprovedCountsAsPositive verifies that user_approved
// entries are returned alongside allow entries and not grouped with deny.
func TestLookupSimilarUserApprovedCountsAsPositive(t *testing.T) {
	c := openTestCorpus(t)
	sig := gitSig()

	// Write one user_approved entry.
	e := sampleEntry(sig, "", "user_approved")
	if err := c.Write(e); err != nil {
		t.Fatalf("Write user_approved: %v", err)
	}

	results, err := c.LookupSimilar([]string{"git"}, sig, defaultLookupConfig(c))
	if err != nil {
		t.Fatalf("LookupSimilar: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Decision != "user_approved" {
		t.Errorf("expected user_approved, got %q", results[0].Decision)
	}
}

// TestLookupSimilarJaccardFilter verifies that entries with dissimilar
// signatures are excluded when MinSimilarity is set above their Jaccard score.
func TestLookupSimilarJaccardFilter(t *testing.T) {
	c := openTestCorpus(t)

	// Write an entry with a "git" signature.
	gitSignature := gitSig()
	e := sampleEntry(gitSignature, "", "allow")
	if err := c.Write(e); err != nil {
		t.Fatalf("Write: %v", err)
	}

	// Lookup with a "curl" signature — Jaccard with git entry should be 0
	// since the tuples are completely different.
	curlSignature := curlSig()
	cfg := LookupConfig{
		MinSimilarity:  0.5, // require at least 50% similarity
		MaxPrecedents:  10,
		MaxPerPolarity: 10,
		MaxAge:         24 * time.Hour,
	}

	// Use both names so the SQL WHERE EXISTS matches (curl not in git entry).
	// Actually git entry has command_names=["git"], lookup by ["git"] to get a
	// candidate but the signature tuples differ — Jaccard should be 0.
	results, err := c.LookupSimilar([]string{"git"}, curlSignature, cfg)
	if err != nil {
		t.Fatalf("LookupSimilar: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results after Jaccard filter, got %d", len(results))
	}
}

// TestLookupSimilarMaxAgeFilter verifies that entries older than MaxAge are
// excluded from results.
func TestLookupSimilarMaxAgeFilter(t *testing.T) {
	c := openTestCorpus(t)
	sig := gitSig()

	// Insert an entry directly with an old created_at timestamp.
	past := time.Now().UTC().Add(-48 * time.Hour).Format(time.RFC3339)
	_, err := c.db.Exec(`
		INSERT INTO precedents (signature, signature_hash, command_names, flags, decision, created_at)
		VALUES (?, ?, '["git"]', '[]', 'allow', ?)`,
		sig, hashString(sig)+"old", past,
	)
	if err != nil {
		t.Fatalf("direct insert: %v", err)
	}

	// MaxAge = 1 hour — the 48h-old entry should not appear.
	cfg := LookupConfig{
		MinSimilarity:  0.0,
		MaxPrecedents:  10,
		MaxPerPolarity: 10,
		MaxAge:         time.Hour,
	}
	results, err := c.LookupSimilar([]string{"git"}, sig, cfg)
	if err != nil {
		t.Fatalf("LookupSimilar: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d (old entries not filtered)", len(results))
	}
}

// TestWriteIdempotentUserApproved verifies that the UNIQUE constraint on
// (stargate_trace_id, decision) WHERE decision='user_approved' prevents
// duplicate user_approved entries for the same trace.
func TestWriteIdempotentUserApproved(t *testing.T) {
	c := openTestCorpus(t)
	sig := gitSig()
	hash := hashString(sig)

	// First user_approved entry.
	e := PrecedentEntry{
		Signature:     sig,
		SignatureHash: hash,
		CommandNames:  []string{"git"},
		Flags:         []string{},
		Decision:      "user_approved",
		TraceID:       "trace-abc",
	}
	if err := c.Write(e); err != nil {
		t.Fatalf("first Write: %v", err)
	}

	// Second insert with same trace_id and decision=user_approved should fail
	// due to UNIQUE constraint.
	e2 := e
	e2.SignatureHash = hash + "2" // different hash to pass per-sig rate limit
	_, err := c.db.Exec(`
		INSERT INTO precedents (signature, signature_hash, command_names, flags, decision, stargate_trace_id)
		VALUES (?, ?, '["git"]', '[]', 'user_approved', 'trace-abc')`,
		sig, hash+"2",
	)
	if err == nil {
		t.Error("expected UNIQUE constraint error for duplicate user_approved trace_id, got nil")
	}
}
