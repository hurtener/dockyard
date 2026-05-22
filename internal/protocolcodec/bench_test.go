package protocolcodec

import (
	"testing"
	"time"
)

// This file holds the Phase 21.5 benchmarks for the protocolcodec hot paths —
// the encode/decode of the MCP extension wire formats. Every tool call and
// every Tasks request crosses this seam, so it is a hot reusable artifact.
// Benchmarks are a baseline + regression-spotting tool, not a CI gate;
// `make bench` runs them on demand.

func benchTask() Task {
	ttl := int64(900000)
	poll := int64(250)
	return Task{
		ID:            "task-bench-0001",
		Status:        TaskWorking,
		StatusMessage: "in progress",
		CreatedAt:     time.Unix(1_700_000_000, 0).UTC(),
		LastUpdatedAt: time.Unix(1_700_000_100, 0).UTC(),
		TTL:           &ttl,
		PollInterval:  &poll,
	}
}

// BenchmarkEncodeTask measures encoding a Tasks Task to its wire JSON.
func BenchmarkEncodeTask(b *testing.B) {
	c := CodecFor(VersionMCP20251125)
	t := benchTask()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := c.EncodeTask(t); err != nil {
			b.Fatalf("EncodeTask: %v", err)
		}
	}
}

// BenchmarkDecodeTask measures decoding a Tasks Task from wire JSON.
func BenchmarkDecodeTask(b *testing.B) {
	c := CodecFor(VersionMCP20251125)
	raw, err := c.EncodeTask(benchTask())
	if err != nil {
		b.Fatalf("EncodeTask: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := c.DecodeTask(raw); err != nil {
			b.Fatalf("DecodeTask: %v", err)
		}
	}
}

// BenchmarkEncodeTaskRoundTrip measures a full encode→decode cycle — the cost a
// Tasks request actually pays crossing the seam in both directions.
func BenchmarkEncodeTaskRoundTrip(b *testing.B) {
	c := CodecFor(VersionMCP20251125)
	t := benchTask()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		raw, err := c.EncodeTask(t)
		if err != nil {
			b.Fatalf("EncodeTask: %v", err)
		}
		if _, err := c.DecodeTask(raw); err != nil {
			b.Fatalf("DecodeTask: %v", err)
		}
	}
}

// BenchmarkEncodeAppsToolMeta measures merging Apps tool metadata into a
// `_meta` map — the per-tool path for a UI-bearing tool.
func BenchmarkEncodeAppsToolMeta(b *testing.B) {
	c := CodecFor(VersionApps20260126)
	m := AppsToolMeta{ResourceURI: "ui://bench/main", Visibility: []string{"model", "app"}}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := c.EncodeAppsToolMeta(nil, m); err != nil {
			b.Fatalf("EncodeAppsToolMeta: %v", err)
		}
	}
}

// BenchmarkDecodeAppsToolMeta measures extracting Apps tool metadata from a
// `_meta` map — the per-tool decode path.
func BenchmarkDecodeAppsToolMeta(b *testing.B) {
	c := CodecFor(VersionApps20260126)
	meta, err := c.EncodeAppsToolMeta(nil, AppsToolMeta{ResourceURI: "ui://bench/main"})
	if err != nil {
		b.Fatalf("EncodeAppsToolMeta: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := c.DecodeAppsToolMeta(meta); err != nil {
			b.Fatalf("DecodeAppsToolMeta: %v", err)
		}
	}
}

// BenchmarkCodecFor measures the version→codec registry lookup — the per-
// request dispatch the runtime does to pick a codec.
func BenchmarkCodecFor(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = CodecFor(VersionMCP20251125)
	}
}
