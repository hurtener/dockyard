/**
 * schema-form.test.ts — covers the JSON-Schema → form-field translator and
 * the form-values → JSON-payload parser that drive the inspector's
 * operator-initiated tool-invocation form (`ToolsPanel.svelte`, D-131).
 */
import { describe, expect, it } from 'vitest';
import {
  fieldsFromSchema,
  initialValues,
  parseFormValues,
} from '../lib/schema-form.js';
import type { ContractSchema } from '../lib/contracts.js';

describe('fieldsFromSchema', () => {
  it('returns a single JSON field for a non-object root', () => {
    const fields = fieldsFromSchema({ type: 'string' });
    expect(fields).toHaveLength(1);
    expect(fields[0].kind).toBe('json');
    expect(fields[0].name).toBe('__root__');
  });

  it('falls back to a JSON field when the schema is missing', () => {
    const fields = fieldsFromSchema(undefined);
    expect(fields[0].kind).toBe('json');
  });

  it('translates an object schema into one field per property', () => {
    const schema: ContractSchema = {
      type: 'object',
      required: ['title', 'count'],
      properties: {
        title: { type: 'string' },
        count: { type: 'integer' },
        enabled: { type: 'boolean' },
        ratio: { type: 'number' },
      },
    };
    const fields = fieldsFromSchema(schema);
    expect(fields.map((f) => f.name)).toEqual([
      'title', 'count', 'enabled', 'ratio',
    ]);
    expect(fields[0].kind).toBe('string');
    expect(fields[0].required).toBe(true);
    expect(fields[1].kind).toBe('integer');
    expect(fields[1].required).toBe(true);
    expect(fields[2].kind).toBe('boolean');
    expect(fields[2].required).toBe(false);
    expect(fields[3].kind).toBe('number');
  });

  it('detects enum choices regardless of declared type', () => {
    const schema: ContractSchema = {
      type: 'object',
      properties: { tone: { type: 'string', enum: ['ok', 'warn', 'error'] } },
    };
    const [tone] = fieldsFromSchema(schema);
    expect(tone.kind).toBe('enum');
    expect(tone.choices?.map((c) => c.value)).toEqual(['ok', 'warn', 'error']);
  });

  it('renders a scalar array as an array field', () => {
    const schema: ContractSchema = {
      type: 'object',
      properties: { tags: { type: 'array', items: { type: 'string' } } },
    };
    const [tags] = fieldsFromSchema(schema);
    expect(tags.kind).toBe('array');
    expect(tags.itemKind).toBe('string');
  });

  it('drops a nested object to a JSON editor', () => {
    const schema: ContractSchema = {
      type: 'object',
      properties: {
        nested: { type: 'object', properties: { a: { type: 'string' } } },
      },
    };
    const [nested] = fieldsFromSchema(schema);
    expect(nested.kind).toBe('json');
  });
});

describe('parseFormValues', () => {
  const schema: ContractSchema = {
    type: 'object',
    required: ['title', 'count'],
    properties: {
      title: { type: 'string' },
      count: { type: 'integer' },
      enabled: { type: 'boolean' },
      tags: { type: 'array', items: { type: 'string' } },
    },
  };
  const fields = fieldsFromSchema(schema);

  it('parses a complete form into a typed payload', () => {
    const values = {
      title: 'Hello',
      count: '7',
      enabled: true,
      tags: 'a, b, c',
    };
    const { arguments: args, errors } = parseFormValues(fields, values);
    expect(errors).toEqual({});
    expect(args).toEqual({
      title: 'Hello',
      count: 7,
      enabled: true,
      tags: ['a', 'b', 'c'],
    });
  });

  it('flags a missing required field', () => {
    const values = initialValues(fields);
    const { errors } = parseFormValues(fields, values);
    expect(Object.keys(errors)).toContain('title');
    expect(Object.keys(errors)).toContain('count');
  });

  it('rejects a non-integer integer field', () => {
    const values = { ...initialValues(fields), title: 'x', count: '1.5' };
    const { errors } = parseFormValues(fields, values);
    expect(errors.count).toMatch(/integer/);
  });

  it('omits blank optional fields from the payload', () => {
    const values = { ...initialValues(fields), title: 'x', count: '1' };
    const { arguments: args, errors } = parseFormValues(fields, values);
    expect(errors).toEqual({});
    expect(args).toEqual({ title: 'x', count: 1, enabled: false });
  });

  it('parses a JSON-root form', () => {
    const jsonField = fieldsFromSchema(undefined);
    const { arguments: args, errors } = parseFormValues(jsonField, {
      __root__: '{"a": 1, "b": "two"}',
    });
    expect(errors).toEqual({});
    expect(args).toEqual({ a: 1, b: 'two' });
  });

  it('rejects an invalid JSON root', () => {
    const jsonField = fieldsFromSchema(undefined);
    const { errors } = parseFormValues(jsonField, { __root__: '{not json' });
    expect(errors.__root__).toMatch(/invalid JSON/);
  });
});
