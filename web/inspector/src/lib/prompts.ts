/**
 * prompts.ts — the inspector's typed view of the attached server's MCP
 * Prompts (v1.1 Wave A; closes D-151).
 *
 * MCP separates two primitives:
 *
 *   - Tools — things the model PUSHES (typed input → typed output).
 *   - Prompts — templates the host PULLS via prompts/get; the host
 *     surfaces them as slash commands / quick-actions to seed a chat.
 *
 * Dockyard supports both. The Tools surface flows through the
 * contract-first schema-form pipeline (P1; D-131). Prompts do NOT — MCP
 * prompt arguments are a flat string-keyed map (D-152), so the panel
 * renders a simple typed form keyed by `PromptArgument.Name`. This module
 * is the typed model + a tolerant parser the panel + the API client share.
 */

/** One argument an MCP Prompt accepts. */
export interface PromptArgumentInfo {
  /** The argument's wire name. */
  name: string;
  /** Optional human-facing label. */
  title?: string;
  /** Optional human-readable description. */
  description?: string;
  /** True when the host must supply this argument. */
  required?: boolean;
}

/** One registered MCP Prompt on the attached server. */
export interface PromptInfo {
  /** The prompt's wire identifier. */
  name: string;
  /** Optional human-facing display label. */
  title?: string;
  /** Optional model-facing summary. */
  description?: string;
  /** The prompt's argument schema; may be empty. */
  arguments: PromptArgumentInfo[];
}

/** One rendered message in a prompts/get response. */
export interface PromptGetMessage {
  /** "user" | "assistant" | "system" — verbatim from the MCP spec. */
  role: string;
  /** The text body, including any rendered substitutions. */
  text: string;
}

/** The body of one operator-initiated prompts/get. */
export interface PromptGetResponse {
  /** Optional description override; falls back to PromptInfo.description. */
  description?: string;
  /** The rendered messages in order. */
  messages: PromptGetMessage[];
  /**
   * Server-side prompts/get error (a successful RPC where the server
   * reported a typed error). The frontend renders the panel's error
   * region without conflating it with a transport-level failure
   * (the 200-with-error pattern D-131 set for tools/invoke; D-163
   * extends it to prompts/get).
   */
  error?: string;
}

/**
 * Tolerantly parses the JSON the backend returns from `GET /api/prompts`.
 * A non-array yields an empty list; entries that lack a `name` are
 * skipped. Unknown fields are ignored — a future richer prompt shape
 * never breaks the panel.
 */
export function parsePrompts(input: unknown): PromptInfo[] {
  if (!Array.isArray(input)) return [];
  const out: PromptInfo[] = [];
  for (const raw of input) {
    if (typeof raw !== 'object' || raw === null) continue;
    const r = raw as Record<string, unknown>;
    if (typeof r.name !== 'string' || r.name === '') continue;
    out.push({
      name: r.name,
      title: typeof r.title === 'string' ? r.title : undefined,
      description: typeof r.description === 'string' ? r.description : undefined,
      arguments: parsePromptArguments(r.arguments),
    });
  }
  return out;
}

/**
 * Tolerantly parses the prompt arguments array. A non-array yields the
 * empty list; entries that lack a `name` are skipped.
 */
export function parsePromptArguments(input: unknown): PromptArgumentInfo[] {
  if (!Array.isArray(input)) return [];
  const out: PromptArgumentInfo[] = [];
  for (const raw of input) {
    if (typeof raw !== 'object' || raw === null) continue;
    const r = raw as Record<string, unknown>;
    if (typeof r.name !== 'string' || r.name === '') continue;
    out.push({
      name: r.name,
      title: typeof r.title === 'string' ? r.title : undefined,
      description: typeof r.description === 'string' ? r.description : undefined,
      required: r.required === true,
    });
  }
  return out;
}

/**
 * Tolerantly parses the JSON the backend returns from
 * `POST /api/prompts/get`. A non-object yields `{messages: []}`. Unknown
 * fields are ignored.
 */
export function parsePromptGetResponse(input: unknown): PromptGetResponse {
  if (typeof input !== 'object' || input === null) {
    return { messages: [] };
  }
  const r = input as Record<string, unknown>;
  const out: PromptGetResponse = {
    description: typeof r.description === 'string' ? r.description : undefined,
    messages: [],
    error: typeof r.error === 'string' ? r.error : undefined,
  };
  if (Array.isArray(r.messages)) {
    for (const raw of r.messages) {
      if (typeof raw !== 'object' || raw === null) continue;
      const m = raw as Record<string, unknown>;
      const role = typeof m.role === 'string' ? m.role : '';
      const text = typeof m.text === 'string' ? m.text : '';
      out.messages.push({ role, text });
    }
  }
  return out;
}
