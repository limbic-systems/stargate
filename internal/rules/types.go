// Package rules defines the command classification types and rule engine.
package rules

import "github.com/limbic-systems/stargate/internal/types"

// CommandInfo represents a single command invocation extracted from the AST.
// Aliased from internal/types to allow other packages to import types without
// importing rules (avoiding circular imports).
type CommandInfo = types.CommandInfo

// RedirectInfo describes a single file redirection.
type RedirectInfo = types.RedirectInfo

// CommandContext describes where a command appears in the AST structure.
type CommandContext = types.CommandContext
