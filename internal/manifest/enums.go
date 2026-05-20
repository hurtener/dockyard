package manifest

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// The manifest's enumerated fields. Each enum type is a string so it round-trips
// through YAML verbatim, and each provides an UnmarshalYAML that rejects an
// unknown value at decode time with a source-located error — so a typo in the
// manifest fails in Dockyard's own tooling, not inside a host.
//
// The enums implement yaml.v3's node-based Unmarshaler — UnmarshalYAML(*yaml.Node)
// — rather than the obsolete yaml.v2-style UnmarshalYAML(func(any) error). The
// node form gives the decode-time rejection the precise source position
// (yaml.Node.Line) of the offending scalar, which load.go's decodeError threads
// into the "file:line" the RFC §4.2 acceptance criterion requires.

// Transport is a deployment mode the app supports (RFC §5.2).
type Transport string

const (
	// TransportStdio is the stdio transport — single-user, local.
	TransportStdio Transport = "stdio"
	// TransportHTTP is the streamable-HTTP transport.
	TransportHTTP Transport = "http"
)

func validTransports() []Transport { return []Transport{TransportStdio, TransportHTTP} }

// UIFramework is the App UI framework (RFC §4.2). V1: svelte only.
type UIFramework string

// UIFrameworkSvelte is Svelte — the only V1 UI framework.
const UIFrameworkSvelte UIFramework = "svelte"

func validUIFrameworks() []UIFramework { return []UIFramework{UIFrameworkSvelte} }

// BundleStrategy is how an App's UI is bundled (RFC §7.4).
type BundleStrategy string

const (
	// BundleSingleFile inlines everything into one HTML file — zero external
	// origins, so the deny-by-default CSP just works. The default.
	BundleSingleFile BundleStrategy = "single-file"
	// BundleMultiFile keeps separate asset files — requires CSP opt-outs.
	BundleMultiFile BundleStrategy = "multi-file"
)

func validBundleStrategies() []BundleStrategy {
	return []BundleStrategy{BundleSingleFile, BundleMultiFile}
}

// TaskSupport is a tool's declared relationship to the MCP Tasks extension
// (RFC §8.4). The zero value is TaskSupportForbidden.
type TaskSupport string

const (
	// TaskSupportForbidden — the tool never runs as a task. The default.
	TaskSupportForbidden TaskSupport = "forbidden"
	// TaskSupportOptional — the tool may run as a task or synchronously.
	TaskSupportOptional TaskSupport = "optional"
	// TaskSupportRequired — the tool always runs as a task.
	TaskSupportRequired TaskSupport = "required"
)

func validTaskSupports() []TaskSupport {
	return []TaskSupport{TaskSupportForbidden, TaskSupportOptional, TaskSupportRequired}
}

// DisplayMode is an App viewing style (RFC §7.2).
type DisplayMode string

const (
	// DisplayModeInline — an inline widget.
	DisplayModeInline DisplayMode = "inline"
	// DisplayModeFullscreen — a fullscreen view.
	DisplayModeFullscreen DisplayMode = "fullscreen"
	// DisplayModePIP — a picture-in-picture view.
	DisplayModePIP DisplayMode = "pip"
)

func validDisplayModes() []DisplayMode {
	return []DisplayMode{DisplayModeInline, DisplayModeFullscreen, DisplayModePIP}
}

// Visibility is a surface an App is exposed to (RFC §7.1, _meta.ui.visibility).
type Visibility string

const (
	// VisibilityModel — the App is offered to the model.
	VisibilityModel Visibility = "model"
	// VisibilityApp — the App is offered to the host application surface.
	VisibilityApp Visibility = "app"
)

func validVisibilities() []Visibility { return []Visibility{VisibilityModel, VisibilityApp} }

// decodeEnum decodes a YAML scalar into an enum value, rejecting any value not
// in valid with an error that names the allowed set. The caller's UnmarshalYAML
// wraps the result with the node position.
func decodeEnum[T ~string](raw string, valid []T) (T, error) {
	for _, v := range valid {
		if string(v) == raw {
			return v, nil
		}
	}
	var zero T
	return zero, fmt.Errorf("unknown value %q, want one of %s", raw, joinEnum(valid))
}

func joinEnum[T ~string](valid []T) string {
	out := ""
	for i, v := range valid {
		if i > 0 {
			out += ", "
		}
		out += string(v)
	}
	return out
}

// UnmarshalYAML decodes and validates a Transport.
func (t *Transport) UnmarshalYAML(node *yaml.Node) error {
	return unmarshalEnum(node, t, validTransports())
}

// UnmarshalYAML decodes and validates a UIFramework.
func (f *UIFramework) UnmarshalYAML(node *yaml.Node) error {
	return unmarshalEnum(node, f, validUIFrameworks())
}

// UnmarshalYAML decodes and validates a BundleStrategy.
func (b *BundleStrategy) UnmarshalYAML(node *yaml.Node) error {
	return unmarshalEnum(node, b, validBundleStrategies())
}

// UnmarshalYAML decodes and validates a TaskSupport.
func (ts *TaskSupport) UnmarshalYAML(node *yaml.Node) error {
	return unmarshalEnum(node, ts, validTaskSupports())
}

// UnmarshalYAML decodes and validates a DisplayMode.
func (d *DisplayMode) UnmarshalYAML(node *yaml.Node) error {
	return unmarshalEnum(node, d, validDisplayModes())
}

// UnmarshalYAML decodes and validates a Visibility.
func (v *Visibility) UnmarshalYAML(node *yaml.Node) error {
	return unmarshalEnum(node, v, validVisibilities())
}

// unmarshalEnum is the shared body of every enum UnmarshalYAML: decode the
// scalar node as a string, then map it through decodeEnum. A rejection carries
// the node's 1-based source line — yaml.v3 surfaces it in the wrapped
// TypeError, and load.go's decodeError recovers it into the "file:line" form.
func unmarshalEnum[T ~string](node *yaml.Node, dst *T, valid []T) error {
	var raw string
	if err := node.Decode(&raw); err != nil {
		return err
	}
	v, err := decodeEnum(raw, valid)
	if err != nil {
		return fmt.Errorf("line %d: %w", node.Line, err)
	}
	*dst = v
	return nil
}
