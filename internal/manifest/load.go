package manifest

import (
	"fmt"
	"io"
	"os"

	"gopkg.in/yaml.v3"
)

// DefaultFilename is the conventional manifest filename at an app's root.
const DefaultFilename = "dockyard.app.yaml"

// LoadFile reads, parses, and structurally validates the manifest at path. It
// is the convenience entry point for the Wave 7 CLI commands: a nil error means
// the returned Manifest is well-formed and ready to consume.
func LoadFile(path string) (*Manifest, error) {
	f, err := os.Open(path) //nolint:gosec // path is a user-supplied manifest location, by design.
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrInvalidManifest, path, err)
	}
	defer func() { _ = f.Close() }()
	return Load(f, path)
}

// Load parses a manifest from r and structurally validates it. source is the
// human-facing origin (a file path, or any identifier) used to source-locate
// errors; it never opens the filesystem itself.
//
// Load fails on: an I/O error reading r, a YAML syntax error, an unknown field,
// a bad enum value, or any structural-validation fault. Every failure wraps
// ErrInvalidManifest and, where a position is known, carries a "file:line".
func Load(r io.Reader, source string) (*Manifest, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, &Error{Source: source, Msg: fmt.Sprintf("read manifest: %v", err)}
	}
	if len(raw) == 0 {
		return nil, &Error{Source: source, Msg: "manifest is empty"}
	}

	// Decode into a document node first so YAML positions are available for
	// source-locating both decode errors and structural-validation faults.
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, decodeError(source, err)
	}

	var m Manifest
	dec := yaml.NewDecoder(byteReader(raw))
	dec.KnownFields(true) // an unknown manifest field is a fault, not silently dropped.
	if err := dec.Decode(&m); err != nil {
		return nil, decodeError(source, err)
	}

	pos := newPositionIndex(&doc)
	if err := m.validate(source, pos); err != nil {
		return nil, err
	}
	return &m, nil
}

// decodeError converts a yaml decode failure into a source-located *Error. The
// yaml.v3 TypeError carries per-field messages with embedded "line N" text;
// decodeError surfaces the first one and recovers the line number from it.
func decodeError(source string, err error) error {
	var te *yaml.TypeError
	if as := errorsAs(err, &te); as && len(te.Errors) > 0 {
		msg := te.Errors[0]
		return &Error{Source: source, Line: lineFromYAMLMessage(msg), Msg: msg}
	}
	return &Error{Source: source, Line: lineFromYAMLMessage(err.Error()), Msg: err.Error()}
}
