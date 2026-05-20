package protocolcodec

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ErrMalformedMeta is returned (wrapped) when a `_meta` value has the wrong
// shape for the extension key it sits under. Surfacing it as an error — rather
// than panicking or silently dropping data — lets Dockyard's own validation
// catch extension-metadata bugs before a host does (brief 03 R7).
var ErrMalformedMeta = errors.New("protocolcodec: malformed extension metadata")

// Codec encodes Dockyard domain types into MCP extension wire formats and
// decodes wire formats back, for one negotiated protocol version. It is the
// only surface through which the rest of Dockyard touches extension wire
// shapes (RFC §5.4). Obtain one with [CodecFor] or [CodecForStrict].
//
// A Codec is stateless and safe for concurrent use by multiple goroutines —
// every method takes its inputs as arguments and returns fresh values. The
// "reusable artifact ⇒ concurrent-reuse test" rule (AGENTS.md §14) is met by
// TestCodecConcurrentReuse.
//
// Decoders are tolerant: an unknown extra key inside a recognised `_meta`
// object is ignored, and the deprecated flat MCP Apps form is accepted.
// Encoders are strict: they emit only the current spec shapes and NEVER the
// deprecated form (RFC §16 item 3).
type Codec interface {
	// Version reports the protocol version this codec serves.
	Version() ProtocolVersion

	// ---- MCP Apps (io.modelcontextprotocol/ui) ----

	// EncodeAppsToolMeta merges the Apps tool metadata into base and returns
	// the resulting `_meta` map. base may be nil; it is never mutated. If m
	// carries no UI information the `ui` key is omitted. The deprecated flat
	// `ui/resourceUri` key is removed if present in base — encoders never
	// emit it.
	EncodeAppsToolMeta(base Meta, m AppsToolMeta) (Meta, error)

	// DecodeAppsToolMeta extracts Apps tool metadata from a `_meta` map. It
	// reads the nested `_meta.ui` form and, if that is absent, falls back to
	// the deprecated flat `_meta["ui/resourceUri"]` form (tolerated on read).
	// A `_meta` with no Apps keys yields a zero AppsToolMeta and ok == false.
	DecodeAppsToolMeta(meta Meta) (m AppsToolMeta, ok bool, err error)

	// EncodeAppsResourceMeta merges the Apps resource metadata into base and
	// returns the resulting `_meta` map. base may be nil and is never
	// mutated. If m is zero the `ui` key is omitted.
	EncodeAppsResourceMeta(base Meta, m AppsResourceMeta) (Meta, error)

	// DecodeAppsResourceMeta extracts Apps resource metadata from a `_meta`
	// map. A `_meta` with no `ui` key yields a zero value and ok == false.
	DecodeAppsResourceMeta(meta Meta) (m AppsResourceMeta, ok bool, err error)

	// EncodeAppsExtensionCapability returns the JSON value for the
	// `capabilities.extensions["io.modelcontextprotocol/ui"]` block.
	EncodeAppsExtensionCapability(c AppsExtensionCapability) (json.RawMessage, error)

	// DecodeAppsExtensionCapability parses an extensions-capability value. A
	// nil/empty input yields a zero value and ok == false.
	DecodeAppsExtensionCapability(raw json.RawMessage) (c AppsExtensionCapability, ok bool, err error)

	// ---- MCP Tasks (io.modelcontextprotocol/tasks) ----

	// EncodeTask returns the JSON of a Tasks `Task` object.
	EncodeTask(t Task) (json.RawMessage, error)

	// DecodeTask parses a Tasks `Task` object.
	DecodeTask(raw json.RawMessage) (Task, error)

	// EncodeTaskMeta returns the JSON value for the request-augmentation
	// `task` field (`TaskMetadata`).
	EncodeTaskMeta(m TaskMeta) (json.RawMessage, error)

	// DecodeTaskMeta parses the request-augmentation `task` field. A
	// nil/empty input yields a zero value and ok == false.
	DecodeTaskMeta(raw json.RawMessage) (m TaskMeta, ok bool, err error)

	// EncodeRelatedTaskMeta merges the `io.modelcontextprotocol/related-task`
	// association key into base and returns the resulting `_meta`. base may
	// be nil and is never mutated.
	EncodeRelatedTaskMeta(base Meta, taskID string) (Meta, error)

	// DecodeRelatedTaskMeta extracts the related-task id from a `_meta` map.
	// A `_meta` without the key yields "" and ok == false.
	DecodeRelatedTaskMeta(meta Meta) (taskID string, ok bool, err error)

	// EncodeCreateTaskResultMeta merges the optional CreateTaskResult `_meta`
	// keys into base and returns the resulting `_meta`. base may be nil and
	// is never mutated. If m is zero, base is returned unchanged.
	EncodeCreateTaskResultMeta(base Meta, m CreateTaskResultMeta) (Meta, error)

	// DecodeCreateTaskResultMeta extracts the optional CreateTaskResult
	// `_meta` keys. A `_meta` without them yields a zero value and
	// ok == false.
	DecodeCreateTaskResultMeta(meta Meta) (m CreateTaskResultMeta, ok bool, err error)

	// EncodeTasksServerCapability returns the JSON value for the
	// `capabilities.tasks` block.
	EncodeTasksServerCapability(c TasksServerCapability) (json.RawMessage, error)

	// DecodeTasksServerCapability parses a `capabilities.tasks` block. A
	// nil/empty input yields a zero value and ok == false.
	DecodeTasksServerCapability(raw json.RawMessage) (c TasksServerCapability, ok bool, err error)
}

// v1Codec is the codec for every protocol version Dockyard V1 supports. The
// Apps (2026-01-26) and Tasks (experimental) wire shapes are stable across
// those versions, so one implementation serves them all; a future spec bump
// adds a sibling codec rather than editing this one (RFC §16).
type v1Codec struct{}

// compile-time assertion that v1Codec satisfies the seam.
var _ Codec = v1Codec{}

func (v1Codec) Version() ProtocolVersion { return DefaultVersion }

// remarshal round-trips v through JSON into dst. It is the single conversion
// primitive used to move between domain values and the unexported wire
// structs, so a marshal/unmarshal bug has one place to live.
func remarshal(v, dst any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, dst)
}

// ---- MCP Apps ----

func (v1Codec) EncodeAppsToolMeta(base Meta, m AppsToolMeta) (Meta, error) {
	out := base.clone()
	if out == nil {
		out = Meta{}
	}
	// Encoders never emit the deprecated flat form: strip it if a caller's
	// base map happened to carry it (RFC §16 item 3).
	delete(out, metaKeyUIResourceURIFlat)
	if !m.hasUI() {
		delete(out, metaKeyUI)
		return out, nil
	}
	out[metaKeyUI] = appsUIToolWire(m)
	return out, nil
}

func (v1Codec) DecodeAppsToolMeta(meta Meta) (AppsToolMeta, bool, error) {
	if meta == nil {
		return AppsToolMeta{}, false, nil
	}
	// Preferred: the nested `_meta.ui` form.
	if raw, present := meta[metaKeyUI]; present {
		var w appsUIToolWire
		if err := remarshal(raw, &w); err != nil {
			return AppsToolMeta{}, false, fmt.Errorf("%w: _meta.ui: %w", ErrMalformedMeta, err)
		}
		m := AppsToolMeta(w)
		if m.hasUI() {
			return m, true, nil
		}
	}
	// Fallback: the DEPRECATED flat `_meta["ui/resourceUri"]` form. Tolerated
	// on read so a tool authored against an older host still links its UI
	// (brief 01 §2.3); Dockyard never writes it back.
	if raw, present := meta[metaKeyUIResourceURIFlat]; present {
		s, ok := raw.(string)
		if !ok {
			return AppsToolMeta{}, false, fmt.Errorf(
				"%w: _meta[%q] is not a string", ErrMalformedMeta, metaKeyUIResourceURIFlat)
		}
		if s != "" {
			return AppsToolMeta{ResourceURI: s}, true, nil
		}
	}
	return AppsToolMeta{}, false, nil
}

func (v1Codec) EncodeAppsResourceMeta(base Meta, m AppsResourceMeta) (Meta, error) {
	out := base.clone()
	if out == nil {
		out = Meta{}
	}
	if m.isZero() {
		delete(out, metaKeyUI)
		return out, nil
	}
	w := appsUIResourceWire{Domain: m.Domain, PrefersBorder: m.PrefersBorder}
	if !m.CSP.isZero() {
		w.CSP = &appsCSPWire{
			ConnectDomains:  m.CSP.ConnectDomains,
			ResourceDomains: m.CSP.ResourceDomains,
			FrameDomains:    m.CSP.FrameDomains,
			BaseURIDomains:  m.CSP.BaseURIDomains,
		}
	}
	if !m.Permissions.isZero() {
		w.Permissions = permissionsToWire(m.Permissions)
	}
	out[metaKeyUI] = w
	return out, nil
}

func (v1Codec) DecodeAppsResourceMeta(meta Meta) (AppsResourceMeta, bool, error) {
	if meta == nil {
		return AppsResourceMeta{}, false, nil
	}
	raw, present := meta[metaKeyUI]
	if !present {
		return AppsResourceMeta{}, false, nil
	}
	var w appsUIResourceWire
	if err := remarshal(raw, &w); err != nil {
		return AppsResourceMeta{}, false, fmt.Errorf("%w: _meta.ui: %w", ErrMalformedMeta, err)
	}
	m := AppsResourceMeta{Domain: w.Domain, PrefersBorder: w.PrefersBorder}
	if w.CSP != nil {
		m.CSP = AppsCSP{
			ConnectDomains:  w.CSP.ConnectDomains,
			ResourceDomains: w.CSP.ResourceDomains,
			FrameDomains:    w.CSP.FrameDomains,
			BaseURIDomains:  w.CSP.BaseURIDomains,
		}
	}
	if w.Permissions != nil {
		m.Permissions = permissionsFromWire(w.Permissions)
	}
	return m, !m.isZero(), nil
}

func permissionsToWire(p AppsPermissions) *appsPermissionsWire {
	w := &appsPermissionsWire{}
	if p.Camera {
		w.Camera = &struct{}{}
	}
	if p.Microphone {
		w.Microphone = &struct{}{}
	}
	if p.Geolocation {
		w.Geolocation = &struct{}{}
	}
	if p.ClipboardWrite {
		w.ClipboardWrite = &struct{}{}
	}
	return w
}

func permissionsFromWire(w *appsPermissionsWire) AppsPermissions {
	return AppsPermissions{
		Camera:         w.Camera != nil,
		Microphone:     w.Microphone != nil,
		Geolocation:    w.Geolocation != nil,
		ClipboardWrite: w.ClipboardWrite != nil,
	}
}

func (v1Codec) EncodeAppsExtensionCapability(c AppsExtensionCapability) (json.RawMessage, error) {
	mimes := c.MIMETypes
	if mimes == nil {
		// `mimeTypes` is REQUIRED by spec; default to the only MVP type
		// rather than emit an invalid empty-key capability.
		mimes = []string{MIMETypeApp}
	}
	return json.Marshal(appsExtensionCapabilityWire{MIMETypes: mimes})
}

func (v1Codec) DecodeAppsExtensionCapability(raw json.RawMessage) (AppsExtensionCapability, bool, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return AppsExtensionCapability{}, false, nil
	}
	var w appsExtensionCapabilityWire
	if err := json.Unmarshal(raw, &w); err != nil {
		return AppsExtensionCapability{}, false, fmt.Errorf(
			"%w: extensions[%q]: %w", ErrMalformedMeta, ExtensionApps, err)
	}
	return AppsExtensionCapability(w), true, nil
}

// ---- MCP Tasks ----

func (v1Codec) EncodeTask(t Task) (json.RawMessage, error) {
	return json.Marshal(taskWire{
		TaskID:        t.ID,
		Status:        t.Status,
		StatusMessage: t.StatusMessage,
		CreatedAt:     t.CreatedAt.UTC().Format(time.RFC3339Nano),
		LastUpdatedAt: t.LastUpdatedAt.UTC().Format(time.RFC3339Nano),
		TTL:           t.TTL,
		PollInterval:  t.PollInterval,
	})
}

func (v1Codec) DecodeTask(raw json.RawMessage) (Task, error) {
	var w taskWire
	if err := json.Unmarshal(raw, &w); err != nil {
		return Task{}, fmt.Errorf("%w: task: %w", ErrMalformedMeta, err)
	}
	if !w.Status.Valid() {
		return Task{}, fmt.Errorf("%w: task: unknown status %q", ErrMalformedMeta, w.Status)
	}
	created, err := parseTaskTime("createdAt", w.CreatedAt)
	if err != nil {
		return Task{}, err
	}
	updated, err := parseTaskTime("lastUpdatedAt", w.LastUpdatedAt)
	if err != nil {
		return Task{}, err
	}
	return Task{
		ID:            w.TaskID,
		Status:        w.Status,
		StatusMessage: w.StatusMessage,
		CreatedAt:     created,
		LastUpdatedAt: updated,
		TTL:           w.TTL,
		PollInterval:  w.PollInterval,
	}, nil
}

func parseTaskTime(field, v string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, v)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: task.%s: %w", ErrMalformedMeta, field, err)
	}
	return t, nil
}

func (v1Codec) EncodeTaskMeta(m TaskMeta) (json.RawMessage, error) {
	return json.Marshal(taskMetadataWire(m))
}

func (v1Codec) DecodeTaskMeta(raw json.RawMessage) (TaskMeta, bool, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return TaskMeta{}, false, nil
	}
	var w taskMetadataWire
	if err := json.Unmarshal(raw, &w); err != nil {
		return TaskMeta{}, false, fmt.Errorf("%w: task: %w", ErrMalformedMeta, err)
	}
	return TaskMeta(w), true, nil
}

func (v1Codec) EncodeRelatedTaskMeta(base Meta, taskID string) (Meta, error) {
	out := base.clone()
	if out == nil {
		out = Meta{}
	}
	out[metaKeyRelatedTask] = relatedTaskWire{TaskID: taskID}
	return out, nil
}

func (v1Codec) DecodeRelatedTaskMeta(meta Meta) (string, bool, error) {
	if meta == nil {
		return "", false, nil
	}
	raw, present := meta[metaKeyRelatedTask]
	if !present {
		return "", false, nil
	}
	var w relatedTaskWire
	if err := remarshal(raw, &w); err != nil {
		return "", false, fmt.Errorf("%w: _meta[%q]: %w", ErrMalformedMeta, metaKeyRelatedTask, err)
	}
	return w.TaskID, true, nil
}

func (v1Codec) EncodeCreateTaskResultMeta(base Meta, m CreateTaskResultMeta) (Meta, error) {
	out := base.clone()
	if m.isZero() {
		return out, nil
	}
	if out == nil {
		out = Meta{}
	}
	out[metaKeyModelImmediateResponse] = m.ModelImmediateResponse
	return out, nil
}

func (v1Codec) DecodeCreateTaskResultMeta(meta Meta) (CreateTaskResultMeta, bool, error) {
	if meta == nil {
		return CreateTaskResultMeta{}, false, nil
	}
	raw, present := meta[metaKeyModelImmediateResponse]
	if !present {
		return CreateTaskResultMeta{}, false, nil
	}
	s, ok := raw.(string)
	if !ok {
		return CreateTaskResultMeta{}, false, fmt.Errorf(
			"%w: _meta[%q] is not a string", ErrMalformedMeta, metaKeyModelImmediateResponse)
	}
	return CreateTaskResultMeta{ModelImmediateResponse: s}, true, nil
}

func (v1Codec) EncodeTasksServerCapability(c TasksServerCapability) (json.RawMessage, error) {
	w := tasksServerCapabilityWire{}
	if c.List {
		w.List = &struct{}{}
	}
	if c.Cancel {
		w.Cancel = &struct{}{}
	}
	if c.ToolsCall {
		w.Requests = &tasksReqCapabilityWire{Tools: &tasksToolsReqWire{Call: &struct{}{}}}
	}
	return json.Marshal(w)
}

func (v1Codec) DecodeTasksServerCapability(raw json.RawMessage) (TasksServerCapability, bool, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return TasksServerCapability{}, false, nil
	}
	var w tasksServerCapabilityWire
	if err := json.Unmarshal(raw, &w); err != nil {
		return TasksServerCapability{}, false, fmt.Errorf(
			"%w: capabilities.tasks: %w", ErrMalformedMeta, err)
	}
	c := TasksServerCapability{
		List:   w.List != nil,
		Cancel: w.Cancel != nil,
	}
	if w.Requests != nil && w.Requests.Tools != nil && w.Requests.Tools.Call != nil {
		c.ToolsCall = true
	}
	return c, true, nil
}
