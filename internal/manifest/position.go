package manifest

import (
	"bytes"
	"errors"
	"io"
	"regexp"
	"strconv"

	"gopkg.in/yaml.v3"
)

// positionIndex maps a manifest field path — the dotted form used in Error.Field,
// e.g. "tools[0].input" — to the 1-based YAML line of that node. It is built
// once per Load by walking the decoded document tree, so a structural-validation
// fault can be source-located precisely even though validation runs on the typed
// struct, not the YAML.
type positionIndex struct {
	lines map[string]int
}

// newPositionIndex walks a decoded YAML document and records the line of every
// scalar, sequence, and mapping node, keyed by its dotted path from the root.
func newPositionIndex(doc *yaml.Node) *positionIndex {
	pi := &positionIndex{lines: map[string]int{}}
	root := doc
	if root != nil && root.Kind == yaml.DocumentNode && len(root.Content) == 1 {
		root = root.Content[0]
	}
	pi.walk("", root)
	return pi
}

// walk records node's line at path and recurses. For a mapping, each key's value
// is indexed at "path.key"; for a sequence, each element at "path[i]".
func (pi *positionIndex) walk(path string, node *yaml.Node) {
	if node == nil {
		return
	}
	if path != "" && node.Line > 0 {
		if _, seen := pi.lines[path]; !seen {
			pi.lines[path] = node.Line
		}
	}
	switch node.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(node.Content); i += 2 {
			key, val := node.Content[i], node.Content[i+1]
			child := key.Value
			if path != "" {
				child = path + "." + key.Value
			}
			// Index the key's own line too, so a fault on a key (vs. its value)
			// still source-locates.
			if key.Line > 0 {
				if _, seen := pi.lines[child]; !seen {
					pi.lines[child] = key.Line
				}
			}
			pi.walk(child, val)
		}
	case yaml.SequenceNode:
		for i, elem := range node.Content {
			pi.walk(path+"["+strconv.Itoa(i)+"]", elem)
		}
	}
}

// line returns the recorded line for a field path, or 0 when none is known —
// in which case the rendered Error degrades to "file" rather than "file:line".
func (pi *positionIndex) line(field string) int {
	if pi == nil {
		return 0
	}
	return pi.lines[field]
}

// yamlLineRe extracts the line number yaml.v3 embeds in decode-error messages,
// e.g. `yaml: line 7: ...` or `line 12: cannot unmarshal ...`.
var yamlLineRe = regexp.MustCompile(`line (\d+)`)

// lineFromYAMLMessage recovers the 1-based line from a yaml.v3 error message,
// or 0 when the message carries no position.
func lineFromYAMLMessage(msg string) int {
	m := yamlLineRe.FindStringSubmatch(msg)
	if m == nil {
		return 0
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0
	}
	return n
}

// byteReader wraps a byte slice as an io.Reader for a second yaml decode pass.
func byteReader(b []byte) io.Reader { return bytes.NewReader(b) }

// errorsAs is errors.As, isolated here so load.go reads without importing
// errors directly for a single call.
func errorsAs(err error, target any) bool { return errors.As(err, target) }
