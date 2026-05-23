/**
 * schema-form.ts — a small JSON-Schema → form-field translator.
 *
 * Drives the inspector's operator-initiated tool-invocation form
 * (`ToolsPanel.svelte`, D-131). Generates a flat list of typed fields from a
 * tool's generated input JSON Schema (P1 — the schema is the source of truth)
 * and translates back from string-valued form inputs to a typed JSON value.
 *
 * The supported subset matches the analytics-widgets contracts' actual shapes
 * — string, number, integer, boolean, enum (string/number), object, and a
 * single-level array of those. Anything richer falls back to a JSON textarea
 * so the operator is never blocked.
 *
 * This is the inspector's own translator — it never reaches into the runtime
 * or the protocol codec (P3).
 */

import type { ContractSchema } from './contracts.js';

/** The widget kinds the generated form renders. */
export type FieldKind =
  | 'string'
  | 'multiline'
  | 'number'
  | 'integer'
  | 'boolean'
  | 'enum'
  | 'array'
  | 'json';

/** One field in the generated form. */
export interface FormField {
  /** The JSON property name (the path inside the parent object). */
  name: string;
  /** A short human label — the property name title-cased. */
  label: string;
  /** The widget kind to render. */
  kind: FieldKind;
  /** True when the property is in the parent object's `required` set. */
  required: boolean;
  /** Enum choices, when kind === 'enum'. */
  choices?: Array<{ value: string; label: string }>;
  /** Item kind for `array` — restricted to scalars. */
  itemKind?: 'string' | 'number' | 'integer' | 'boolean';
  /** The schema description, when present, surfaced as field help text. */
  description?: string;
}

/**
 * Translates a JSON-Schema object into a flat field list. A non-object root
 * (a primitive input schema, an array root) collapses to a single `json`
 * field so the operator can still type a value into the form.
 */
export function fieldsFromSchema(schema?: ContractSchema): FormField[] {
  if (!schema || schema.type !== 'object' || !schema.properties) {
    return [{
      name: '__root__',
      label: 'Arguments (JSON)',
      kind: 'json',
      required: false,
    }];
  }
  const required = new Set(schema.required ?? []);
  const fields: FormField[] = [];
  for (const [name, sub] of Object.entries(schema.properties)) {
    fields.push(fieldFromSubschema(name, sub, required.has(name)));
  }
  return fields;
}

/** Maps one property schema to its [FormField]. */
function fieldFromSubschema(
  name: string,
  schema: ContractSchema,
  required: boolean,
): FormField {
  const label = humanise(name);
  const description = schemaDescription(schema);
  if (Array.isArray(schema.enum) && schema.enum.length > 0) {
    return {
      name,
      label,
      kind: 'enum',
      required,
      choices: schema.enum.map((v) => ({
        value: String(v),
        label: String(v),
      })),
      ...(description !== undefined && { description }),
    };
  }
  switch (schema.type) {
    case 'boolean':
      return { name, label, kind: 'boolean', required,
        ...(description !== undefined && { description }) };
    case 'integer':
      return { name, label, kind: 'integer', required,
        ...(description !== undefined && { description }) };
    case 'number':
      return { name, label, kind: 'number', required,
        ...(description !== undefined && { description }) };
    case 'string':
      return {
        name,
        label,
        // The generated contracts mark long-form strings via `format`. The
        // operator UI uses multiline for descriptions and HTML-ish bodies.
        kind: schema.format === 'multiline' ? 'multiline' : 'string',
        required,
        ...(description !== undefined && { description }),
      };
    case 'array': {
      const items = schema.items;
      const itemType = items?.type;
      if (
        itemType === 'string' ||
        itemType === 'number' ||
        itemType === 'integer' ||
        itemType === 'boolean'
      ) {
        return {
          name,
          label,
          kind: 'array',
          itemKind: itemType,
          required,
          ...(description !== undefined && { description }),
        };
      }
      return { name, label, kind: 'json', required,
        ...(description !== undefined && { description }) };
    }
    case 'object':
      // A nested object is too rich for the V1 form — drop to a JSON editor
      // so the operator is never blocked. The generated schema still drives
      // the parent form's required-field marking.
      return { name, label, kind: 'json', required,
        ...(description !== undefined && { description }) };
    default:
      return { name, label, kind: 'json', required,
        ...(description !== undefined && { description }) };
  }
}

/** The schema's optional `description`, when present. */
function schemaDescription(schema: ContractSchema): string | undefined {
  const d = (schema as { description?: unknown }).description;
  return typeof d === 'string' && d !== '' ? d : undefined;
}

/** "metric_id" → "Metric id"; "rowCount" → "Row count". */
function humanise(name: string): string {
  const split = name
    .replace(/[_-]+/g, ' ')
    .replace(/([a-z])([A-Z])/g, '$1 $2')
    .toLowerCase();
  return split.charAt(0).toUpperCase() + split.slice(1);
}

/**
 * The form's mutable state, keyed by field name. The boolean kind stores a
 * boolean; everything else stores the raw text the operator typed so we can
 * tell "blank" from "zero".
 */
export type FormValues = Record<string, string | boolean>;

/** A blank initial value for each generated field — the form's default. */
export function initialValues(fields: FormField[]): FormValues {
  const values: FormValues = {};
  for (const f of fields) {
    values[f.name] = f.kind === 'boolean' ? false : '';
  }
  return values;
}

/**
 * One operator-typed value, parsed into the JSON shape its schema declares.
 * A blank required field is reported via [parseFormValues]'s `errors`; a
 * blank optional field is omitted from the result object so the server's
 * schema validation sees a clean payload.
 */
export interface ParseResult {
  /** The parsed JSON-object payload for `arguments`. */
  arguments: Record<string, unknown>;
  /** Validation errors, keyed by field name. */
  errors: Record<string, string>;
}

/** Translates form values to a typed JSON payload, surfacing per-field errors. */
export function parseFormValues(
  fields: FormField[],
  values: FormValues,
): ParseResult {
  const args: Record<string, unknown> = {};
  const errors: Record<string, string> = {};
  for (const f of fields) {
    const v = values[f.name];
    if (f.name === '__root__' && f.kind === 'json') {
      // The whole input is a single JSON blob — parse and return as-is.
      const text = typeof v === 'string' ? v.trim() : '';
      if (text === '') return { arguments: {}, errors: {} };
      try {
        const parsed = JSON.parse(text);
        if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) {
          return {
            arguments: {},
            errors: { __root__: 'arguments must be a JSON object' },
          };
        }
        return { arguments: parsed as Record<string, unknown>, errors: {} };
      } catch (err) {
        return {
          arguments: {},
          errors: { __root__: `invalid JSON: ${(err as Error).message}` },
        };
      }
    }

    const parsed = parseOne(f, v);
    if (parsed.error !== undefined) {
      errors[f.name] = parsed.error;
      continue;
    }
    if (parsed.skip) continue;
    args[f.name] = parsed.value;
  }
  return { arguments: args, errors };
}

interface OneParse {
  value?: unknown;
  /** Omit the property entirely (optional + blank). */
  skip?: boolean;
  /** Per-field error message, when validation failed. */
  error?: string;
}

function parseOne(field: FormField, raw: string | boolean): OneParse {
  if (field.kind === 'boolean') {
    return { value: Boolean(raw) };
  }
  const text = typeof raw === 'string' ? raw.trim() : '';
  if (text === '') {
    if (field.required) return { error: `${field.label} is required` };
    return { skip: true };
  }
  switch (field.kind) {
    case 'string':
    case 'multiline':
      return { value: text };
    case 'integer': {
      const n = Number(text);
      if (!Number.isFinite(n) || !Number.isInteger(n)) {
        return { error: `${field.label} must be an integer` };
      }
      return { value: n };
    }
    case 'number': {
      const n = Number(text);
      if (!Number.isFinite(n)) {
        return { error: `${field.label} must be a number` };
      }
      return { value: n };
    }
    case 'enum':
      return { value: coerceEnumValue(text) };
    case 'array': {
      const items = text
        .split(/[,\n]/)
        .map((s) => s.trim())
        .filter((s) => s !== '');
      const out: unknown[] = [];
      for (const item of items) {
        const parsed = parseScalar(item, field.itemKind ?? 'string');
        if (parsed.error !== undefined) {
          return { error: `${field.label}: ${parsed.error}` };
        }
        out.push(parsed.value);
      }
      return { value: out };
    }
    case 'json':
      try {
        return { value: JSON.parse(text) };
      } catch (err) {
        return { error: `${field.label}: invalid JSON — ${(err as Error).message}` };
      }
    default:
      return { value: text };
  }
}

function parseScalar(
  text: string,
  kind: 'string' | 'number' | 'integer' | 'boolean',
): OneParse {
  switch (kind) {
    case 'string':
      return { value: text };
    case 'integer': {
      const n = Number(text);
      if (!Number.isFinite(n) || !Number.isInteger(n)) {
        return { error: `"${text}" is not an integer` };
      }
      return { value: n };
    }
    case 'number': {
      const n = Number(text);
      if (!Number.isFinite(n)) return { error: `"${text}" is not a number` };
      return { value: n };
    }
    case 'boolean': {
      if (text === 'true') return { value: true };
      if (text === 'false') return { value: false };
      return { error: `"${text}" is not a boolean (use true / false)` };
    }
  }
}

/**
 * Enum values were rendered as strings in the form. We try to coerce back to
 * a number when the value parses as one — the schema's enum constraint will
 * still match. A non-numeric value is returned as-is (a string enum).
 */
function coerceEnumValue(text: string): unknown {
  const n = Number(text);
  if (text !== '' && Number.isFinite(n) && String(n) === text) return n;
  return text;
}
