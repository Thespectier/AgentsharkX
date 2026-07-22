import { readFile, writeFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";

import { format, resolveConfig } from "prettier";
import { parse } from "yaml";

const webRoot = fileURLToPath(new URL("../", import.meta.url));
const specificationPath = fileURLToPath(new URL("../../../api/openapi.yaml", import.meta.url));
const outputPath = fileURLToPath(new URL("../src/generated/api-client.ts", import.meta.url));

const specification = parse(await readFile(specificationPath, "utf8"));
if (specification?.openapi !== "3.1.0") {
  throw new Error("api/openapi.yaml must use OpenAPI 3.1.0");
}

const schemas = specification.components?.schemas ?? {};
const operations = [];
for (const [path, pathItem] of Object.entries(specification.paths ?? {})) {
  for (const method of ["get", "post", "put", "patch", "delete"]) {
    const operation = pathItem?.[method];
    if (!operation || operation["x-implementation-status"] !== "implemented") continue;
    if (!operation.operationId)
      throw new Error(`${method.toUpperCase()} ${path} has no operationId`);
    const success = Object.entries(operation.responses ?? {}).find(([status]) =>
      /^2\d\d$/.test(status),
    );
    if (!success) throw new Error(`${operation.operationId} has no success response`);
    const [, response] = success;
    const responseType = response?.content
      ? schemaType(Object.values(response.content)[0]?.schema)
      : "undefined";
    const bodyType = operation.requestBody?.content
      ? schemaType(Object.values(operation.requestBody.content)[0]?.schema)
      : undefined;
    operations.push({
      id: operation.operationId,
      method: method.toUpperCase(),
      path,
      responseType,
      bodyType,
    });
  }
}
operations.sort((left, right) => left.id.localeCompare(right.id));

const schemaDeclarations = Object.entries(schemas)
  .map(([name, schema]) => `export type ${name} = ${schemaType(schema)};`)
  .join("\n\n");
const operationDefinitions = operations
  .map(
    ({ id, method, path }) =>
      `  ${id}: { method: ${JSON.stringify(method)}, path: ${JSON.stringify(path)} },`,
  )
  .join("\n");
const responseDefinitions = operations
  .map(({ id, responseType }) => `  ${id}: ${responseType};`)
  .join("\n");
const bodyOperations = operations.filter((operation) => operation.bodyType);
const bodyDefinitions = bodyOperations
  .map(({ id, bodyType }) => `  ${id}: ${bodyType};`)
  .join("\n");

const prettierConfig = (await resolveConfig(outputPath)) ?? {};
const source = await format(
  `// Generated from api/openapi.yaml by scripts/generate-api-client.mjs. Do not edit.\n\n${schemaDeclarations}\n\nexport const implementedOperations = {\n${operationDefinitions}\n} as const;\n\nexport interface OperationResponses {\n${responseDefinitions}\n}\n\nexport interface OperationBodies {\n${bodyDefinitions}\n}\n\nexport type ImplementedOperationId = keyof typeof implementedOperations;\nexport type OperationResponse<K extends ImplementedOperationId> = OperationResponses[K];\nexport type OperationBody<K extends keyof OperationBodies> = OperationBodies[K];\n`,
  { ...prettierConfig, filepath: outputPath },
);

const mode = process.argv[2] ?? "--write";
if (mode === "--write") {
  await writeFile(outputPath, source);
} else if (mode === "--check") {
  const existing = await readFile(outputPath, "utf8").catch(() => "");
  if (existing !== source) {
    console.error(`Generated API client is stale. Run: npm --prefix ${webRoot} run api:generate`);
    process.exitCode = 1;
  } else {
    console.log("generated API client: ok");
  }
} else {
  throw new Error(`unknown mode: ${mode}`);
}

function schemaType(schema) {
  if (!schema) return "unknown";
  if (schema.$ref) return schema.$ref.split("/").at(-1);
  if (schema.enum) return schema.enum.map((value) => JSON.stringify(value)).join(" | ");
  if (Array.isArray(schema.type)) {
    return schema.type.map((type) => primitiveType(type, schema)).join(" | ");
  }
  return primitiveType(schema.type, schema);
}

function primitiveType(type, schema) {
  if (type === "null") return "null";
  if (type === "string") return "string";
  if (type === "number" || type === "integer") return "number";
  if (type === "boolean") return "boolean";
  if (type === "array") return `Array<${schemaType(schema.items)}>`;
  if (type === "object" || schema.properties || schema.additionalProperties !== undefined) {
    const required = new Set(schema.required ?? []);
    const members = Object.entries(schema.properties ?? {}).map(([name, property]) => {
      const key = /^[A-Za-z_$][\w$]*$/.test(name) ? name : JSON.stringify(name);
      return `${key}${required.has(name) ? "" : "?"}: ${schemaType(property)};`;
    });
    if (schema.additionalProperties === true) members.push("[key: string]: unknown;");
    if (schema.additionalProperties && typeof schema.additionalProperties === "object") {
      members.push(`[key: string]: ${schemaType(schema.additionalProperties)};`);
    }
    return `{ ${members.join(" ")} }`;
  }
  return "unknown";
}
