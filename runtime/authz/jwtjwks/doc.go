// Package jwtjwks implements signed JWT validation using RFC 8414 discovery and
// a bounded JWKS cache. Importing it registers the "jwt-jwks" authz driver.
//
// Config.MaxStale enables bounded stale-if-error validation for known keys when
// an expired cache cannot refresh due to transport errors, timeouts, HTTP 5xx,
// or HTTP 429. Rejected documents and other HTTP statuses fail closed. Set it to
// DefaultMaxStale for a 24-hour window, or zero to disable fallback. Failed
// refresh attempts remain subject to RefreshCooldown measured from completion
// and never extend the last successful set's lifetime.
// Unknown key IDs and invalid signatures still require successful refresh and
// verification; JWT claim checks are never relaxed. A successful refresh
// immediately replaces all cached keys, including removal of revoked keys.
// During an outage, removed keys can remain trusted until CacheTTL + MaxStale
// after the start of the last successful fetch. Stale-eligible callers do not
// wait behind an in-flight refresh. Content-free slog Warn/Info messages mark
// entry into stale mode and recovery; per-request details remain at Debug.
package jwtjwks
