package manifest

import "fmt"

// The manifest's enumerated fields. Each enum type is a string so it round-trips
// through YAML verbatim, and each provides an UnmarshalYAML that rejects an
// unknown value at decode time with a source-located error — so a typo in the
// manifest fails in Dockyard's own tooling, not inside a host.

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
func (t *Transport) UnmarshalYAML(unmarshal func(any) error) error {
	return unmarshalEnum(unmarshal, t, validTransports())
}

// UnmarshalYAML decodes and validates a UIFramework.
func (f *UIFramework) UnmarshalYAML(unmarshal func(any) error) error {
	return unmarshalEnum(unmarshal, f, validUIFrameworks())
}

// UnmarshalYAML decodes and validates a BundleStrategy.
func (b *BundleStrategy) UnmarshalYAML(unmarshal func(any) error) error {
	return unmarshalEnum(unmarshal, b, validBundleStrategies())
}

// UnmarshalYAML decodes and validates a TaskSupport.
func (ts *TaskSupport) UnmarshalYAML(unmarshal func(any) error) error {
	return unmarshalEnum(unmarshal, ts, validTaskSupports())
}

// UnmarshalYAML decodes and validates a DisplayMode.
func (d *DisplayMode) UnmarshalYAML(unmarshal func(any) error) error {
	return unmarshalEnum(unmarshal, d, validDisplayModes())
}

// UnmarshalYAML decodes and validates a Visibility.
func (v *Visibility) UnmarshalYAML(unmarshal func(any) error) error {
	return unmarshalEnum(unmarshal, v, validVisibilities())
}

// unmarshalEnum is the shared body of every enum UnmarshalYAML: decode the
// scalar as a string, then map it through decodeEnum.
func unmarshalEnum[T ~string](unmarshal func(any) error, dst *T, valid []T) error {
	var raw string
	if err := unmarshal(&raw); err != nil {
		return err
	}
	v, err := decodeEnum(raw, valid)
	if err != nil {
		return err
	}
	*dst = v
	return nil
}
