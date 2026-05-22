// Package cli is the cobra command tree for the dockyard CLI (RFC §9).
//
// The CLI is one statically-linked, CGo-free binary — no npx, no Node, no
// package fan-out (RFC §9.1, brief 04 §3). It is a multi-verb tool built on
// spf13/cobra, the stack settled in RFC §9.3 (brief 06 §2.5): subcommands,
// generated help, shell completions, and gh/kubectl-familiar ergonomics.
//
// Phase 17 shipped the command tree and `dockyard new`, the no-template project
// scaffold (RFC §9.1, §10); Phase 18 adds `generate` and `validate` (RFC §6,
// §9.4). The remaining verbs land in later Wave 7 phases and each registers
// itself onto the same root:
//
//   - `generate`, `validate`        — Phase 18 (RFC §6, §9.4) — landed
//   - `dev`                         — Phase 19 (RFC §9.2)
//   - `build`, `run`, `install`     — Phase 20 (RFC §14)
//   - `test`                        — Phase 21 (RFC §9.1, §9.4)
//   - `inspect`                     — the inspector phase (RFC §12)
//
// The extension contract is deliberately simple: a later phase adds one file
// holding a `func newXxxCmd() *cobra.Command` constructor and one line in
// [NewRootCmd] that calls `root.AddCommand(newXxxCmd())`. No phase restructures
// the tree; each verb is self-contained.
package cli
