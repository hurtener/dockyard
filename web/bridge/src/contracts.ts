/**
 * contracts.ts — the typed-contract shape the bridge consumes.
 *
 * Dockyard is contract-first (P1): a tool's input and output are typed Go
 * structs, and `dockyard generate` (Phase 06, RFC §6) emits a `contracts.ts`
 * that the App's UI imports. This file defines the *shape* that generated
 * `contracts.ts` must satisfy, so the bridge can consume it without depending
 * on codegen output that does not exist yet (Phase 06 not landed — D-061).
 *
 * The bridge never hand-writes a tool contract; it only declares the generic
 * interface generated code conforms to. When Phase 06 lands, its `contracts.ts`
 * must structurally satisfy `ToolContract` for the typed `tool-result` path to
 * hold.
 */

/**
 * A single tool's typed input/output contract. `dockyard generate` emits one
 * `ToolContract` per App tool. The bridge uses `I`/`O` as phantom type carriers
 * — at runtime the object only needs to identify the tool by name.
 */
export interface ToolContract<I = unknown, O = unknown> {
  /** The tool name as registered on the MCP server. */
  readonly name: string;
  /** Phantom carrier for the input type — never read at runtime. */
  readonly __input?: I;
  /** Phantom carrier for the structuredContent output type. */
  readonly __output?: O;
}

/** Extracts the input type of a `ToolContract`. */
export type ContractInput<C> = C extends ToolContract<infer I, unknown>
  ? I
  : never;

/** Extracts the structuredContent output type of a `ToolContract`. */
export type ContractOutput<C> = C extends ToolContract<unknown, infer O>
  ? O
  : never;

/**
 * A generated `contracts.ts` exports a `contracts` object keyed by tool name.
 * The bridge accepts any object of this shape.
 */
export type ContractMap = Readonly<Record<string, ToolContract>>;

/**
 * Defines a typed tool contract. Generated code calls this so the contract
 * carries its `I`/`O` types through to `callTool` and `onToolResult`.
 */
export function defineContract<I, O>(name: string): ToolContract<I, O> {
  return { name };
}
