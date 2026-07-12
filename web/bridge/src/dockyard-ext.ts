/**
 * dockyard-ext.ts — Dockyard's Tasks×Apps `ui/` extensions (D-183).
 *
 * These notifications are the explicit legacy Tasks×Apps adapter
 * surface (RFC §8, D-134) but are **NOT in the vendored MCP Apps schema** — they
 * are Dockyard extensions. They are fenced here, separate from the conformed
 * `protocol.ts` wire surface, for two reasons:
 *
 *  1. The wire-conformance test (`conformance.test.ts`, D-182) asserts these are
 *     the *only* methods the bridge speaks that are absent from the schema — so
 *     the extension boundary is explicit and cannot silently grow.
 *  2. They are **not portable**. A stock MCP Apps host (e.g. Claude Desktop)
 *     does not implement them: `task-progress` is never forwarded (so
 *     `onTaskProgress` never fires) and `elicitation-response` is ignored.
 *     Tasks-augmented App behaviour works only against a Dockyard-aware host —
 *     the inspector, or Harbor as the MCP client. The published docs say so.
 *
 * A future upstream MCP Apps Tasks integration would let these migrate from
 * here into the conformed surface.
 */

/** Dockyard-extension `ui/` notification methods (outside the MCP Apps schema). */
export const DockyardExtMethod = {
  /**
   * `ui/notifications/task-progress` (Host → View) — one progress point of a
   * long-running task. The Dockyard runtime emits each `TaskHandle.Progress`
   * call as an `obs/v1` `task.progress` event; a Dockyard-aware host forwards
   * those to the View. Advisory: a host that does not forward task progress
   * simply never sends it (capability-driven degradation — RFC §7.5).
   */
  taskProgress: 'ui/notifications/task-progress',
  /**
   * `ui/notifications/elicitation-response` (View → host) — the App's reply to a
     * task's legacy `input_required` prompt. Modern peers use `tasks/update`.
   */
  elicitationResponse: 'ui/notifications/elicitation-response',
} as const;

/**
 * The exhaustive set of `ui/` wire methods the bridge speaks that are **not** in
 * the vendored MCP Apps schema. `conformance.test.ts` pins the boundary to this
 * list (D-183).
 */
export const DOCKYARD_EXT_METHODS: readonly string[] = [
  DockyardExtMethod.taskProgress,
  DockyardExtMethod.elicitationResponse,
];

/**
 * `ui/notifications/task-progress` params — one progress point of a
 * long-running task (RFC §8.4). Mirrors the Dockyard runtime's `obs/v1`
 * `task.progress` payload: a `TaskHandle.Progress(fraction, message)` call
 * carries both; a `TaskHandle.Status(message)` call carries the message and
 * omits the fraction (a phase change a fraction cannot express).
 *
 * Every field but `taskId` is optional so a host can forward whatever it
 * has — an App reading the value renders defensively (no fraction ⇒ render
 * the message alone; no message ⇒ render the percentage alone).
 */
export interface TaskProgressParams {
  /** The task this progress point belongs to. */
  taskId: string;
  /**
   * The completion fraction in [0, 1], when known. Absent for a
   * status-only update. An App renders `Math.round(fraction * 100)` as the
   * percentage.
   */
  fraction?: number;
  /** An optional human-readable progress note. */
  message?: string;
  /** The task's lifecycle status at this point (e.g. `working`). */
  status?: string;
}

/**
 * `ui/notifications/elicitation-response` params — the App's reply to a
 * task's `input_required` prompt. The host forwards `data` to the attached
 * server's legacy input relay. It must not be used with modern Tasks peers.
 *
 * `data` is opaque to the bridge — it is the App's contract with its
 * server-side handler. A `request_approval` App posts
 * `{ approved: boolean, reason?: string, decided_at: string }`; a
 * `propose_with_edits` App posts
 * `{ approved: boolean, edits: object | null, decided_at: string }`.
 * The Dockyard runtime's `TaskHandle.RequireInput` returns this verbatim
 * as the `InputResponse.Data` raw JSON for the handler to decode against
 * its own contract.
 *
 * `declined` is the explicit "the user declined to provide input rather
 * than supplying it" signal (the runtime's `InputResponse.Declined`).
 * A declined response is not the same as `approved=false` on a
 * request_approval — declining is the user closing the prompt without
 * deciding; rejecting is a real decision. Handlers route them
 * differently.
 */
export interface ElicitationResponseParams {
  /**
   * The task id whose `input_required` prompt this response answers.
   * Read by the App from the `tool-result` push that opened the
   * elicitation (the runtime stamps it via the related-task `_meta` key).
   */
  taskId: string;
  /**
   * The user's reply, an opaque JSON value the handler decodes. Absent
   * when `declined` is true.
   */
  data?: unknown;
  /**
   * True when the user explicitly declined to answer. The Dockyard runtime
   * receives this as `InputResponse.Declined=true`; the handler decides
   * how to proceed.
   */
  declined?: boolean;
}
