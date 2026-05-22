/**
 * contracts.ts — the inspector's read-only model of a server's generated
 * tool contracts.
 *
 * Dockyard is contract-first (CLAUDE.md §1 / P1): a tool's input and output
 * are typed Go structs, and `dockyard generate` produces the JSON Schema for
 * each. The inspector's fixture switcher derives its six synthetic fixtures
 * FROM these generated contracts (`fixtures.ts`) — never hand-written — so a
 * fixture is always a structurally valid instance of the contract it stands
 * for. This module is the contract shape the inspector consumes and a small
 * tolerant parser for it.
 *
 * The inspector decodes only the JSON-Schema subset the fixture generator
 * needs (object/array/string/number/boolean/integer + properties + required +
 * enum) and tolerates the rest — a richer schema never breaks the inspector.
 */

/** The JSON-Schema subset the inspector's fixture generator understands. */
export interface ContractSchema {
  /** The JSON-Schema `type` — "object" | "array" | "string" | … */
  type?: string;
  /** Object property schemas, keyed by property name. */
  properties?: Record<string, ContractSchema>;
  /** Required object property names. */
  required?: string[];
  /** Array item schema. */
  items?: ContractSchema;
  /** Enum value set — the first member seeds a fixture. */
  enum?: unknown[];
  /** A schema `format` qualifier (e.g. `date-time`). */
  format?: string;
}

/** One generated tool contract — its output schema drives the fixtures. */
export interface ToolContract {
  /** The tool name. */
  name: string;
  /** A short human description. */
  description?: string;
  /** The generated JSON Schema for the tool's input struct. */
  inputSchema?: ContractSchema;
  /** The generated JSON Schema for the tool's output (`structuredContent`). */
  outputSchema?: ContractSchema;
}

/** True when `value` is a structurally plausible {@link ContractSchema}. */
export function isContractSchema(value: unknown): value is ContractSchema {
  return typeof value === 'object' && value !== null;
}

/** Parses one tool contract from an unknown JSON value, tolerating extras. */
export function parseToolContract(value: unknown): ToolContract | null {
  if (typeof value !== 'object' || value === null) return null;
  const v = value as Record<string, unknown>;
  if (typeof v.name !== 'string' || v.name === '') return null;
  return {
    name: v.name,
    description: typeof v.description === 'string' ? v.description : undefined,
    inputSchema: isContractSchema(v.inputSchema)
      ? (v.inputSchema as ContractSchema)
      : undefined,
    outputSchema: isContractSchema(v.outputSchema)
      ? (v.outputSchema as ContractSchema)
      : undefined,
  };
}

/** Parses a `/api/contracts` JSON array into {@link ToolContract}s. */
export function parseContracts(json: unknown): ToolContract[] {
  if (!Array.isArray(json)) return [];
  return json
    .map(parseToolContract)
    .filter((c): c is ToolContract => c !== null);
}
