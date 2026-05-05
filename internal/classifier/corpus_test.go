package classifier_test

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/limbic-systems/stargate/internal/classifier"
	"github.com/limbic-systems/stargate/internal/config"
	"github.com/limbic-systems/stargate/internal/parser"
	"github.com/limbic-systems/stargate/internal/rules"
)

// TestCorpus classifies every command in the testdata corpus files against the
// real stargate.toml config and asserts the expected decision. All failures are
// reported together (t.Errorf, not t.Fatalf) so a single run surfaces the full
// set of divergences.
func TestCorpus(t *testing.T) {
	cfg, err := config.Load("../../stargate.toml")
	if err != nil {
		t.Fatalf("failed to load stargate.toml: %v", err)
	}

	clf, err := classifier.New(cfg)
	if err != nil {
		t.Fatalf("classifier init failed: %v", err)
	}

	files := []string{
		"../../testdata/red_commands.txt",
		"../../testdata/green_commands.txt",
		"../../testdata/yellow_commands.txt",
		"../../testdata/evasion_commands.txt",
	}

	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("read %s: %v", file, err)
			}

			scanner := bufio.NewScanner(strings.NewReader(string(data)))
			scanner.Buffer(make([]byte, 0, 128*1024), 128*1024) // 128KB to exceed max_command_length
			lineNum := 0
			for scanner.Scan() {
				lineNum++
				line := strings.TrimSpace(scanner.Text())

				// Skip blank lines and comments.
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}

				// Each line is: <command><TAB><expected_decision>
				parts := strings.SplitN(line, "\t", 2)
				if len(parts) != 2 {
					t.Errorf("line %d: malformed entry (expected tab-separated command and decision): %q", lineNum, line)
					continue
				}

				command := strings.TrimSpace(parts[0])
				want := strings.TrimSpace(parts[1])

				resp := clf.Classify(context.Background(), classifier.ClassifyRequest{Command: command})
				if resp.Decision != want {
					t.Errorf("line %d: %q => got %q, want %q (reason: %s)",
						lineNum, command, resp.Decision, want, resp.Reason)
				}
			}

			if err := scanner.Err(); err != nil {
				t.Fatalf("scan %s: %v", file, err)
			}
		})
	}
}

// TestYellowLLMReview verifies that YELLOW rules for package managers have
// llm_review=true in the real stargate.toml config. Catches regressions where
// a rule accidentally drops LLM review.
func TestYellowLLMReview(t *testing.T) {
	cfg, err := config.Load("../../stargate.toml")
	if err != nil {
		t.Fatalf("failed to load stargate.toml: %v", err)
	}

	engine, err := rules.NewEngine(cfg)
	if err != nil {
		t.Fatalf("rules engine init failed: %v", err)
	}

	walkerCfg := parser.NewWalkerConfig(cfg.Wrappers, cfg.Commands)

	tests := []struct {
		command string
	}{
		{"bun install express"},
		{"uv add requests"},
		{"uvx black ."},
		{"pipx run ruff check ."},
		{"pydoc json"},
		{"npm install lodash"},
		{"npx prettier ."},
	}

	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			file, err := parser.Parse(tt.command, cfg.Parser.Dialect)
			if err != nil {
				t.Fatalf("parse %q: %v", tt.command, err)
			}
			cmds := parser.Walk(file, walkerCfg)
			if len(cmds) == 0 {
				t.Fatalf("walk %q: no commands found", tt.command)
			}
			result := engine.Evaluate(context.Background(), cmds, tt.command, "")
			if result.Decision != "yellow" {
				t.Errorf("expected yellow, got %s (reason: %s)", result.Decision, result.Reason)
			}
			if result.Rule == nil || result.Rule.Level != "yellow" {
				t.Errorf("expected explicit yellow rule match, got default fallback (reason: %s)", result.Reason)
			}
			if !result.LLMReview {
				t.Errorf("expected LLMReview=true, got false (reason: %s)", result.Reason)
			}
		})
	}
}
