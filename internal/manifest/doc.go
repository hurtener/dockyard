// Package manifest is the typed schema, loader, and structural validator for
// dockyard.app.yaml — Dockyard's control plane (RFC §4.2).
//
// Every Dockyard app has a manifest at its root. It is the single artifact the
// Wave 7 CLI commands — validate, generate, dev, test, build, install — read; it
// makes the conventions explicit and is the one place the tool-to-UI wiring is
// visible. Phase 06 ships the schema + loader + validation only; it deliberately
// leaves a clean, typed Manifest API for those later commands to consume.
//
// # Loading
//
// Load and LoadFile parse YAML into the typed Manifest struct. A malformed
// document fails with an error that names the source and, where the YAML decoder
// reports a node position, the line — so an invalid manifest fails in Dockyard's
// own tooling rather than deep inside a host (RFC §4.2 acceptance).
//
// # Validation
//
// Manifest.Validate runs structural validation: required identity fields,
// well-formed semantic version, known transports and enums, unique tool and app
// names, well-formed ui:// URIs, and tool->app ui: cross-references. Every
// rejection is reported as a source-located *Error (or an *ErrorList for several
// at once). Validation is structural only — it never reads Go source or the
// filesystem; the quality.* gates (RFC §9.4) are parsed and shape-checked here
// but enforced by dockyard validate (Phase 18).
//
// # Contract resolution
//
// A tool's input and output are Go type references (RFC §6.1, contract-first);
// the manifest never duplicates schema. ResolveContracts turns each reference
// into a JSON Schema through the ContractResolver seam. Phase 06 ships a
// registry-backed resolver over internal/codegen.SchemaForType; the
// source-scanning resolver dockyard generate needs is Phase 18's, and satisfies
// the same one-method seam.
package manifest
