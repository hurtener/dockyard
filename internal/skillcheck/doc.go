// Package skillcheck validates SKILL.md files against the agentskills.io
// Agent Skills specification (https://agentskills.io/specification).
//
// Dockyard ships an agent-skill set under skills/ that teaches an AI coding
// agent how to build MCP Apps with Dockyard end to end (Phase 29, AGENTS.md
// §19). A malformed SKILL.md would silently break agent discovery, so the
// format is enforced mechanically: this package powers the drift-audit hook
// and the phase-29 smoke check, both of which fail CI when a SKILL.md does
// not conform.
//
// The validator is read-only and pure: it parses the YAML frontmatter and
// the Markdown body, applies the spec's constraints (name shape, length
// limits, required-fields, body non-empty), and returns a typed Report.
// It does NOT load files outside the skill directory and does NOT execute
// any code referenced by the skill.
//
// Validation rules implemented (spec sections cross-referenced):
//
//   - frontmatter: YAML, opens and closes with `---`, parses as a map.
//   - `name`: required; 1..64 chars; only lowercase a-z, 0-9 and `-`; no
//     leading/trailing hyphen; no consecutive hyphens; matches the parent
//     directory name (the "skill slug" invariant).
//   - `description`: required; 1..1024 chars; non-empty.
//   - `license`, `compatibility`, `metadata`, `allowed-tools`: optional;
//     when present, validated for shape per the spec.
//   - body: non-empty Markdown content after the frontmatter — a SKILL.md
//     with empty body is not a skill, it is a stub.
//
// The cmd/skillcheck binary walks a directory and runs Validate over every
// skill it finds; drift-audit invokes it via `go run`.
package skillcheck
