# MCP Apps 2026-07-28 revision audit

**Audited:** 2026-07-11

The current MCP Apps prose snapshot remains the stable `2026-01-26` revision
at `298e884ec3f02daba085acdb02042d73bd00b355` (2026-01-26). The corresponding
machine-readable schema remains pinned at `7d4434e` (2026-06-01). No revised
Apps snapshot was published for the core `2026-07-28` release candidate.

The Apps extension remains independently versioned. Phase 33 must negotiate it
through the declared extension capability and retain its existing dual-pin
reconciliation requirement; it must not infer an Apps wire revision from the
core protocol version.
