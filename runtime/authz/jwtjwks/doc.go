// Package jwtjwks implements signed JWT validation using RFC 8414 discovery and
// a bounded JWKS cache. Importing it registers the "jwt-jwks" authz driver.
//
// Config.MaxStale enables bounded stale-if-error validation for known keys when
// an expired cache cannot refresh. Set it to DefaultMaxStale for a 24-hour
// window, or zero to disable fallback. Failed refresh attempts remain subject
// to RefreshCooldown and never extend the last successful set's lifetime.
// Unknown key IDs and invalid signatures still require successful refresh and
// verification; JWT claim checks are never relaxed. A successful refresh
// immediately replaces all cached keys, including removal of revoked keys.
// During an outage, removed keys can remain trusted until CacheTTL + MaxStale
// after the last successful fetch. A content-free slog debug message records
// use of stale verification keys without exposing token or claim values.
package jwtjwks
