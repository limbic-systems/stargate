// Package corpus implements the SQLite-backed precedent corpus for storing
// past LLM classification judgments and user approval feedback.
package corpus

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/limbic-systems/stargate/internal/config"
	"github.com/limbic-systems/stargate/internal/ttlmap"

	_ "modernc.org/sqlite" // SQLite driver
)

// Corpus is an SQLite-backed store of past classification judgments.
type Corpus struct {
	db              *sql.DB
	cfg             config.CorpusConfig
	cancel          context.CancelFunc
	wg              sync.WaitGroup
	sigRateLimit    *ttlmap.TTLMap[string, struct{}]
	globalRateLimit *ttlmap.TTLMap[string, int]
	rateMu          sync.Mutex // guards rate-limit check+set in Write
}

// Open creates or opens the corpus database at the configured path.
// Background pruning runs until ctx is cancelled or Close is called.
func Open(ctx context.Context, cfg config.CorpusConfig) (*Corpus, error) {
	dbPath := cfg.Path
	if dbPath == "" {
		return nil, fmt.Errorf("corpus: path is required")
	}

	// Expand ~ to home directory.
	if len(dbPath) > 1 && dbPath[:2] == "~/" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("corpus: expand home dir: %w", err)
		}
		dbPath = filepath.Join(home, dbPath[2:])
	}

	// Create parent directory with 0700 and tighten if it already exists.
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("corpus: create directory %q: %w", dir, err)
	}
	os.Chmod(dir, 0700) //nolint:errcheck // best-effort tighten
	checkPermissions(dir)

	// Open SQLite database.
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("corpus: open %q: %w", dbPath, err)
	}

	// One connection per Corpus instance. Cross-process concurrency (server +
	// CLI) is handled by busy_timeout below, not by connection pooling.
	db.SetMaxOpenConns(1)

	// busy_timeout first: the journal_mode switch below may need to acquire
	// an exclusive lock (e.g., checkpointing an existing WAL), so the
	// timeout must be in effect before that runs.
	if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
		db.Close()
		return nil, fmt.Errorf("corpus: set busy_timeout: %w", err)
	}

	// Only switch journal mode if not already DELETE — avoids taking an
	// exclusive lock on every Open() when the DB was already migrated.
	var mode string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		db.Close()
		return nil, fmt.Errorf("corpus: query journal_mode: %w", err)
	}
	if mode != "delete" {
		if err := db.QueryRow("PRAGMA journal_mode=DELETE").Scan(&mode); err != nil {
			db.Close()
			return nil, fmt.Errorf("corpus: set journal_mode: %w", err)
		}
		if mode != "delete" {
			db.Close()
			return nil, fmt.Errorf("corpus: journal_mode switch failed (got %q, want delete)", mode)
		}
	}

	pragmas := []string{
		// Corpus is a cache of past judgments — losing one entry on OS crash
		// is acceptable. NORMAL avoids redundant fsync on every commit.
		"PRAGMA synchronous=NORMAL",
		// 4KB pages (default) are fine for small row counts.
		"PRAGMA page_size=4096",
		// 64 pages = 256KB in-memory page cache.
		"PRAGMA cache_size=-256",
		// Temp tables/indices in memory — avoids temp file creation which
		// could fail under disk pressure.
		"PRAGMA temp_store=MEMORY",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			db.Close()
			return nil, fmt.Errorf("corpus: %s: %w", p, err)
		}
	}

	// Create schema.
	if err := createSchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("corpus: create schema: %w", err)
	}

	// Tighten file permissions to 0600 before checking — SQLite may have
	// created the file with umask-derived permissions (e.g. 0644).
	os.Chmod(dbPath, 0600) //nolint:errcheck

	// Warn if permissions are still looser than 0600 (e.g. chmod failed).
	checkPermissions(dbPath)

	ctx, cancel := context.WithCancel(ctx)
	c := &Corpus{
		db:     db,
		cfg:    cfg,
		cancel: cancel,
	}

	// Initialize rate limiters.
	c.initRateLimiters(ctx)

	// Start background pruning.
	c.wg.Add(1)
	go c.pruneLoop(ctx)

	return c, nil
}

// Close shuts down the corpus in strict order:
// 1. Cancel context (signals goroutines to stop)
// 2. Wait for goroutines to exit
// 3. WAL checkpoint (only if in WAL mode — skipped for DELETE mode)
// 4. Close database
func (c *Corpus) Close() error {
	c.cancel()
	c.wg.Wait()

	var mode string
	if err := c.db.QueryRow("PRAGMA journal_mode").Scan(&mode); err == nil && mode == "wal" {
		if _, err := c.db.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
			fmt.Fprintf(os.Stderr, "corpus: WAL checkpoint warning: %v\n", err)
		}
	}

	return c.db.Close()
}

// DB returns the underlying database connection for use by write/lookup operations.
func (c *Corpus) DB() *sql.DB {
	return c.db
}

// Config returns the corpus configuration.
func (c *Corpus) Config() config.CorpusConfig {
	return c.cfg
}

func createSchema(db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS precedents (
		id              INTEGER PRIMARY KEY AUTOINCREMENT,
		signature       TEXT    NOT NULL,
		signature_hash  TEXT    NOT NULL,
		raw_command     TEXT,
		command_names   TEXT    NOT NULL,
		flags           TEXT    NOT NULL,
		ast_summary     TEXT,
		cwd             TEXT,
		decision        TEXT    NOT NULL,
		reasoning       TEXT,
		risk_factors    TEXT,
		matched_rule    TEXT,
		scopes_in_play  TEXT,
		stargate_trace_id TEXT,
		created_at      TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
		last_hit_at     TEXT,
		hit_count       INTEGER NOT NULL DEFAULT 0,
		session_id      TEXT,
		agent           TEXT
	);

	CREATE INDEX IF NOT EXISTS idx_precedents_hash    ON precedents (signature_hash);
	CREATE INDEX IF NOT EXISTS idx_precedents_created ON precedents (created_at);
	CREATE INDEX IF NOT EXISTS idx_precedents_decision ON precedents (decision);
	CREATE INDEX IF NOT EXISTS idx_precedents_trace   ON precedents (stargate_trace_id);

	CREATE UNIQUE INDEX IF NOT EXISTS idx_precedents_trace_decision
		ON precedents (stargate_trace_id, decision)
		WHERE decision = 'user_approved';

	-- Note: idx_precedents_commands on the raw command_names TEXT column is
	-- intentionally omitted. The json_each() virtual table join used by
	-- LookupSimilar cannot leverage a B-tree index on the JSON column.
	`
	_, err := db.Exec(schema)
	return err
}

func checkPermissions(path string) {
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	perm := info.Mode().Perm()
	if perm&0077 != 0 {
		expected := "0600"
		if info.IsDir() {
			expected = "0700"
		}
		fmt.Fprintf(os.Stderr, "corpus: WARNING: %s has permissions %o (expected %s). Other users may be able to access classification data.\n", path, perm, expected)
	}
}

func (c *Corpus) pruneLoop(ctx context.Context) {
	defer c.wg.Done()

	interval := time.Hour // default
	if c.cfg.PruneInterval != "" {
		if d, err := time.ParseDuration(c.cfg.PruneInterval); err == nil && d > 0 {
			interval = d
		}
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.prune()
		}
	}
}

func (c *Corpus) prune() {
	// Prune by age.
	if c.cfg.MaxAge != "" {
		if maxAge, err := config.ParseMaxAge(c.cfg.MaxAge); err == nil && maxAge > 0 {
			cutoff := time.Now().UTC().Add(-maxAge).Format(time.RFC3339)
			c.db.Exec("DELETE FROM precedents WHERE created_at < ?", cutoff) //nolint:errcheck
		}
	}

	// Prune by count.
	if maxEntries := c.cfg.GetMaxEntries(); maxEntries > 0 {
		c.db.Exec(`
			DELETE FROM precedents WHERE id NOT IN (
				SELECT id FROM precedents ORDER BY created_at DESC LIMIT ?
			)
		`, maxEntries) //nolint:errcheck
	}
}
