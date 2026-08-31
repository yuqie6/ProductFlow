/**
 * 评测用 JSON Schema 子集校验（$ref / $defs / required / additionalProperties / anyOf）。
 * 只覆盖全局 draft_schema 实际用到的关键字，不引入 Ajv。
 */

import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

export interface JSONSchema {
  $ref?: string;
  $defs?: Record<string, JSONSchema>;
  type?: string | string[];
  const?: unknown;
  required?: string[];
  additionalProperties?: boolean | JSONSchema;
  properties?: Record<string, JSONSchema>;
  items?: JSONSchema | JSONSchema[];
  anyOf?: JSONSchema[];
  minItems?: number;
  maxItems?: number;
  minLength?: number;
  maxLength?: number;
  minimum?: number;
  maximum?: number;
}

let cachedGlobalDraftSchema: JSONSchema | undefined;

export function loadGlobalDraftSchema(): JSONSchema {
  if (!cachedGlobalDraftSchema) {
    const path = resolve(dirname(fileURLToPath(import.meta.url)), "../../go/internal/agent/global_draft_schema.json");
    cachedGlobalDraftSchema = JSON.parse(readFileSync(path, "utf8")) as JSONSchema;
  }
  return cachedGlobalDraftSchema;
}

export function checkJSONSchema(schema: JSONSchema, value: unknown): boolean {
  return matchSchema(schema, value, schema);
}

function matchSchema(schema: JSONSchema, value: unknown, root: JSONSchema): boolean {
  if (schema.$ref) {
    return matchSchema(resolveRef(schema.$ref, root), value, root);
  }
  if (schema.anyOf) {
    return schema.anyOf.some((item) => matchSchema(item, value, root));
  }
  if (schema.const !== undefined && !Object.is(schema.const, value)) {
    return false;
  }
  if (schema.type !== undefined && !matchType(schema.type, value)) {
    return false;
  }
  if (typeof value === "string") {
    if (schema.minLength !== undefined && value.length < schema.minLength) return false;
    if (schema.maxLength !== undefined && value.length > schema.maxLength) return false;
  }
  if (typeof value === "number") {
    if (schema.minimum !== undefined && value < schema.minimum) return false;
    if (schema.maximum !== undefined && value > schema.maximum) return false;
  }
  if (Array.isArray(value)) {
    if (schema.minItems !== undefined && value.length < schema.minItems) return false;
    if (schema.maxItems !== undefined && value.length > schema.maxItems) return false;
    if (schema.items) {
      const items = schema.items;
      if (Array.isArray(items)) {
        if (value.length !== items.length) return false;
        return value.every((item, index) => matchSchema(items[index], item, root));
      }
      return value.every((item) => matchSchema(items, item, root));
    }
    return true;
  }
  if (value !== null && typeof value === "object") {
    const record = value as Record<string, unknown>;
    for (const key of schema.required ?? []) {
      if (!Object.hasOwn(record, key)) return false;
    }
    const properties = schema.properties ?? {};
    for (const [key, child] of Object.entries(record)) {
      const propertySchema = properties[key];
      if (propertySchema) {
        if (!matchSchema(propertySchema, child, root)) return false;
        continue;
      }
      if (schema.additionalProperties === false) return false;
      if (schema.additionalProperties && typeof schema.additionalProperties === "object") {
        if (!matchSchema(schema.additionalProperties, child, root)) return false;
      }
    }
  }
  return true;
}

function matchType(type: string | string[], value: unknown): boolean {
  const types = Array.isArray(type) ? type : [type];
  return types.some((item) => {
    switch (item) {
      case "object":
        return value !== null && typeof value === "object" && !Array.isArray(value);
      case "array":
        return Array.isArray(value);
      case "string":
        return typeof value === "string";
      case "integer":
        return typeof value === "number" && Number.isInteger(value);
      case "number":
        return typeof value === "number" && Number.isFinite(value);
      case "boolean":
        return typeof value === "boolean";
      case "null":
        return value === null;
      default:
        return false;
    }
  });
}

function resolveRef(ref: string, root: JSONSchema): JSONSchema {
  if (!ref.startsWith("#/")) {
    throw new Error(`unsupported JSON Schema $ref ${ref}`);
  }
  let current: unknown = root;
  for (const part of ref.slice(2).split("/")) {
    if (!current || typeof current !== "object") {
      throw new Error(`unresolved JSON Schema $ref ${ref}`);
    }
    current = (current as Record<string, unknown>)[part];
  }
  if (!current || typeof current !== "object" || Array.isArray(current)) {
    throw new Error(`unresolved JSON Schema $ref ${ref}`);
  }
  return current as JSONSchema;
}
