import { createHash } from "node:crypto";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const lockPath = resolve(root, "apps/web/package-lock.json");
const goModPath = resolve(root, "apps/server/go.mod");
const goSumPath = resolve(root, "apps/server/go.sum");
const pythonProjectPath = resolve(root, "sdk/python/pyproject.toml");
const pythonConstraintsPath = resolve(root, "sdk/python/constraints.txt");
const versionsPath = resolve(root, "deploy/versions.env");
const releaseDir = resolve(root, "docs/release");
const lockText = await readFile(lockPath, "utf8");
const lock = JSON.parse(lockText);
const goModText = await readFile(goModPath, "utf8");
const goSumText = await readFile(goSumPath, "utf8");
const pythonProjectText = await readFile(pythonProjectPath, "utf8");
const pythonConstraintsText = await readFile(pythonConstraintsPath, "utf8");
const versions = Object.fromEntries(
  (await readFile(versionsPath, "utf8"))
    .split("\n")
    .filter((line) => line && !line.startsWith("#") && line.includes("="))
    .map((line) => {
      const separator = line.indexOf("=");
      return [line.slice(0, separator), line.slice(separator + 1)];
    }),
);
const agentGuardRevision = versions.AGENTGUARD_GIT_REVISION ?? "";
if (!/^[0-9a-f]{40}$/i.test(agentGuardRevision)) {
  throw new Error(
    "deploy/versions.env must contain a full AgentGuard Git revision",
  );
}

const dependenciesByIdentity = new Map();
for (const [location, metadata] of Object.entries(lock.packages ?? {})) {
  if (!location.startsWith("node_modules/") || !metadata.version) continue;
  const name = npmPackageName(location);
  const identity = `${name}\u0000${metadata.version}`;
  const existing = dependenciesByIdentity.get(identity);
  if (existing) {
    if (
      existing.resolved !== metadata.resolved ||
      existing.integrity !== metadata.integrity
    ) {
      throw new Error(
        `conflicting npm package metadata for ${name}@${metadata.version}`,
      );
    }
    if (!metadata.dev) existing.scope = "runtime";
    continue;
  }
  dependenciesByIdentity.set(identity, {
    name,
    version: metadata.version,
    license: normalizeLicense(metadata.license),
    scope: metadata.dev ? "build" : "runtime",
    resolved: metadata.resolved,
    integrity: metadata.integrity,
  });
}
const dependencies = [...dependenciesByIdentity.values()];
dependencies.sort((left, right) => left.name.localeCompare(right.name));

// This manifest is the reviewed linux/amd64 runtime module graph for
// ./cmd/agentshark. Keeping it explicit makes generation independent of module
// downloads and prevents test-only modules from entering the release SBOM.
const goRuntimeDefinitions = [
  {
    path: "github.com/jackc/pgpassfile",
    version: "v1.0.0",
    license: "MIT",
    licenseSource: "https://github.com/jackc/pgpassfile/blob/v1.0.0/LICENSE",
  },
  {
    path: "github.com/jackc/pgservicefile",
    version: "v0.0.0-20240606120523-5a60cdf6a761",
    license: "MIT",
    licenseSource:
      "https://github.com/jackc/pgservicefile/blob/5a60cdf6a761/LICENSE",
  },
  {
    path: "github.com/jackc/pgx/v5",
    version: "v5.10.0",
    license: "MIT",
    licenseSource: "https://github.com/jackc/pgx/blob/v5.10.0/LICENSE",
    direct: true,
    dependencies: [
      "github.com/jackc/pgpassfile",
      "github.com/jackc/pgservicefile",
      "github.com/jackc/puddle/v2",
      "golang.org/x/sync",
      "golang.org/x/text",
    ],
  },
  {
    path: "github.com/jackc/puddle/v2",
    version: "v2.2.2",
    license: "MIT",
    licenseSource: "https://github.com/jackc/puddle/blob/v2.2.2/LICENSE",
  },
  {
    path: "golang.org/x/sync",
    version: "v0.22.0",
    license: "BSD-3-Clause",
    licenseSource: "https://github.com/golang/sync/blob/v0.22.0/LICENSE",
  },
  {
    path: "golang.org/x/text",
    version: "v0.40.0",
    license: "BSD-3-Clause",
    licenseSource: "https://github.com/golang/text/blob/v0.40.0/LICENSE",
  },
  {
    path: "github.com/grpc-ecosystem/grpc-gateway/v2",
    version: "v2.29.0",
    license: "BSD-3-Clause",
    licenseSource: "https://github.com/grpc-ecosystem/grpc-gateway/blob/v2.29.0/LICENSE.txt",
    dependencies: [
      "golang.org/x/net",
      "golang.org/x/sys",
      "golang.org/x/text",
      "google.golang.org/genproto/googleapis/api",
      "google.golang.org/genproto/googleapis/rpc",
      "google.golang.org/grpc",
      "google.golang.org/protobuf",
    ],
  },
  {
    path: "go.opentelemetry.io/proto/otlp",
    version: "v1.11.0",
    license: "Apache-2.0",
    licenseSource: "https://github.com/open-telemetry/opentelemetry-proto-go/blob/v1.11.0/LICENSE",
    direct: true,
    dependencies: [
      "github.com/grpc-ecosystem/grpc-gateway/v2",
      "google.golang.org/grpc",
      "google.golang.org/protobuf",
    ],
  },
  {
    path: "golang.org/x/net",
    version: "v0.57.0",
    license: "BSD-3-Clause",
    licenseSource: "https://github.com/golang/net/blob/v0.57.0/LICENSE",
    dependencies: ["golang.org/x/sys", "golang.org/x/text"],
  },
  {
    path: "golang.org/x/sys",
    version: "v0.47.0",
    license: "BSD-3-Clause",
    licenseSource: "https://github.com/golang/sys/blob/v0.47.0/LICENSE",
  },
  {
    path: "google.golang.org/genproto/googleapis/api",
    version: "v0.0.0-20260720211330-0afa2a65878a",
    license: "Apache-2.0",
    licenseSource: "https://github.com/googleapis/go-genproto/blob/0afa2a65878a/LICENSE",
    dependencies: ["google.golang.org/protobuf"],
  },
  {
    path: "google.golang.org/genproto/googleapis/rpc",
    version: "v0.0.0-20260720211330-0afa2a65878a",
    license: "Apache-2.0",
    licenseSource: "https://github.com/googleapis/go-genproto/blob/0afa2a65878a/LICENSE",
    dependencies: ["google.golang.org/protobuf"],
  },
  {
    path: "google.golang.org/grpc",
    version: "v1.82.1",
    license: "Apache-2.0",
    licenseSource: "https://github.com/grpc/grpc-go/blob/v1.82.1/LICENSE",
    dependencies: [
      "golang.org/x/net",
      "golang.org/x/sys",
      "google.golang.org/genproto/googleapis/rpc",
      "google.golang.org/protobuf",
    ],
  },
  {
    path: "google.golang.org/protobuf",
    version: "v1.36.11",
    license: "BSD-3-Clause",
    licenseSource: "https://github.com/protocolbuffers/protobuf-go/blob/v1.36.11/LICENSE",
    direct: true,
  },
].sort((left, right) => left.path.localeCompare(right.path));
const goRequirements = parseGoRequirements(goModText);
const goSums = parseGoSums(goSumText);
validateGoRuntimeManifest(goRuntimeDefinitions, goRequirements, goSums);

const pythonDirectDependencies = new Set([
  "openinference-instrumentation",
  "openinference-instrumentation-langchain",
  "openinference-instrumentation-mcp",
  "opentelemetry-api",
  "opentelemetry-exporter-otlp-proto-http",
  "opentelemetry-sdk",
]);
const pythonRuntimeDefinitions = [
  ["certifi", "2026.7.22", "MPL-2.0"],
  ["charset-normalizer", "3.4.9", "MIT"],
  ["googleapis-common-protos", "1.75.0", "Apache-2.0"],
  ["idna", "3.18", "BSD-3-Clause"],
  ["openinference-instrumentation", "0.1.54", "Apache-2.0"],
  ["openinference-instrumentation-langchain", "0.1.67", "Apache-2.0"],
  ["openinference-instrumentation-mcp", "2.0.3", "Apache-2.0"],
  ["openinference-semantic-conventions", "0.1.30", "Apache-2.0"],
  ["opentelemetry-api", "1.44.0", "Apache-2.0"],
  ["opentelemetry-exporter-otlp-proto-common", "1.44.0", "Apache-2.0"],
  ["opentelemetry-exporter-otlp-proto-http", "1.44.0", "Apache-2.0"],
  ["opentelemetry-instrumentation", "0.65b0", "Apache-2.0"],
  ["opentelemetry-proto", "1.44.0", "Apache-2.0"],
  ["opentelemetry-sdk", "1.44.0", "Apache-2.0"],
  ["opentelemetry-semantic-conventions", "0.65b0", "Apache-2.0"],
  ["packaging", "26.2", "Apache-2.0 OR BSD-2-Clause"],
  ["protobuf", "7.35.1", "BSD-3-Clause"],
  ["requests", "2.34.2", "Apache-2.0"],
  ["typing-extensions", "4.16.0", "PSF-2.0"],
  ["urllib3", "2.7.0", "MIT"],
  ["wrapt", "2.3.0", "BSD-2-Clause"],
]
  .map(([name, version, license]) => ({
    name,
    version,
    license,
    direct: pythonDirectDependencies.has(name),
  }))
  .sort((left, right) => left.name.localeCompare(right.name));
const pythonConstraints = parsePythonConstraints(pythonConstraintsText);
validatePythonRuntimeManifest(
  pythonRuntimeDefinitions,
  pythonConstraints,
  pythonProjectText,
);

const releaseVersion = versions.AGENTSHARK_VERSION ?? "0.9.0-preview";
const created = "2026-07-29T00:00:00Z";
const rootPackages = [
  packageEntry(
    "AgentsharkX server",
    releaseVersion,
    "Apache-2.0",
    "APPLICATION",
  ),
  packageEntry(
    "AgentsharkX web",
    lock.packages?.[""]?.version ?? "0.1.0",
    "Apache-2.0",
    "APPLICATION",
  ),
  packageEntry(
    "AgentsharkX Python SDK",
    "0.1.0",
    "Apache-2.0",
    "LIBRARY",
  ),
];
const npmPackages = dependencies.map((dependency) => ({
  ...packageEntry(
    dependency.name,
    dependency.version,
    dependency.license,
    "LIBRARY",
  ),
  SPDXID: spdxID(`npm-${dependency.name}-${dependency.version}`),
  downloadLocation: dependency.resolved ?? "NOASSERTION",
  externalRefs: [
    {
      referenceCategory: "PACKAGE-MANAGER",
      referenceType: "purl",
      referenceLocator: npmPURL(dependency.name, dependency.version),
    },
  ],
  annotations: [
    {
      annotationDate: created,
      annotationType: "OTHER",
      annotator: "Tool: AgentsharkX release artifact generator",
      comment: `${dependency.scope} dependency from apps/web/package-lock.json`,
    },
  ],
}));
const goPackages = goRuntimeDefinitions.map((dependency) => ({
  ...packageEntry(
    dependency.path,
    dependency.version,
    dependency.license,
    "LIBRARY",
  ),
  SPDXID: spdxID(`go-${dependency.path}-${dependency.version}`),
  downloadLocation: `https://proxy.golang.org/${dependency.path}/@v/${dependency.version}.zip`,
  externalRefs: [
    {
      referenceCategory: "PACKAGE-MANAGER",
      referenceType: "purl",
      referenceLocator: `pkg:golang/${dependency.path}@${dependency.version}`,
    },
  ],
  annotations: [
    {
      annotationDate: created,
      annotationType: "OTHER",
      annotator: "Tool: AgentsharkX release artifact generator",
      comment: `${dependency.direct ? "direct" : "transitive"} runtime dependency from apps/server/go.mod; go.sum ${goSums.get(dependency.path)?.get(dependency.version)}; declared license verified at ${dependency.licenseSource}`,
    },
  ],
}));
const pythonPackages = pythonRuntimeDefinitions.map((dependency) => ({
  ...packageEntry(
    dependency.name,
    dependency.version,
    dependency.license,
    "LIBRARY",
  ),
  SPDXID: spdxID(`pypi-${dependency.name}-${dependency.version}`),
  externalRefs: [
    {
      referenceCategory: "PACKAGE-MANAGER",
      referenceType: "purl",
      referenceLocator: `pkg:pypi/${dependency.name}@${dependency.version}`,
    },
  ],
  annotations: [
    {
      annotationDate: created,
      annotationType: "OTHER",
      annotator: "Tool: AgentsharkX release artifact generator",
      comment: `${dependency.direct ? "direct" : "transitive"} runtime dependency from sdk/python/constraints.txt; license reviewed from installed distribution metadata`,
    },
  ],
}));
const upstreamPackages = [
  {
    ...packageEntry(
      "agentgateway",
      versions.AGENTGATEWAY_VERSION ?? "NOASSERTION",
      "Apache-2.0",
      "APPLICATION",
    ),
    SPDXID: "SPDXRef-Upstream-agentgateway",
    downloadLocation: "https://github.com/agentgateway/agentgateway",
    supplier: "Organization: agentgateway",
  },
  {
    ...packageEntry(
      "AgentGuard",
      agentGuardRevision,
      "GPL-3.0-only",
      "APPLICATION",
    ),
    SPDXID: "SPDXRef-Upstream-AgentGuard",
    downloadLocation: `https://github.com/WhitzardAgent/AgentGuard/tree/${agentGuardRevision}`,
    supplier: "Organization: WhitzardAgent",
    annotations: [
      {
        annotationDate: created,
        annotationType: "OTHER",
        annotator: "Tool: AgentsharkX release artifact generator",
        comment: `Pinned source revision for separately installed AgentGuard; preview image label ${versions.AGENTGUARD_VERSION ?? "NOASSERTION"}`,
      },
    ],
  },
  {
    ...packageEntry(
      "PostgreSQL",
      versions.POSTGRES_VERSION ?? "NOASSERTION",
      "PostgreSQL",
      "APPLICATION",
    ),
    SPDXID: "SPDXRef-Upstream-PostgreSQL",
    downloadLocation: "https://hub.docker.com/_/postgres",
    supplier: "Organization: PostgreSQL Global Development Group",
  },
];

const goPackageByPath = new Map(
  goRuntimeDefinitions.map((dependency, index) => [
    dependency.path,
    goPackages[index],
  ]),
);

const document = {
  spdxVersion: "SPDX-2.3",
  dataLicense: "CC0-1.0",
  SPDXID: "SPDXRef-DOCUMENT",
  name: `AgentsharkX-${releaseVersion}`,
  documentNamespace: `https://github.com/Thespectier/AgentsharkX/sbom/${releaseVersion}`,
  creationInfo: {
    created,
    creators: ["Tool: AgentsharkX release artifact generator"],
    licenseListVersion: "3.26",
  },
  documentDescribes: rootPackages.map((item) => item.SPDXID),
  packages: [
    ...rootPackages,
    ...npmPackages,
    ...goPackages,
    ...pythonPackages,
    ...upstreamPackages,
  ],
  relationships: [
    ...npmPackages.map((item) => ({
      spdxElementId: rootPackages[1].SPDXID,
      relationshipType: "DEPENDS_ON",
      relatedSpdxElement: item.SPDXID,
    })),
    ...goRuntimeDefinitions
      .filter((dependency) => dependency.direct)
      .map((dependency) => ({
        spdxElementId: rootPackages[0].SPDXID,
        relationshipType: "DEPENDS_ON",
        relatedSpdxElement: goPackageByPath.get(dependency.path).SPDXID,
      })),
    ...goRuntimeDefinitions.flatMap((dependency) =>
      (dependency.dependencies ?? []).map((relatedPath) => ({
        spdxElementId: goPackageByPath.get(dependency.path).SPDXID,
        relationshipType: "DEPENDS_ON",
        relatedSpdxElement: goPackageByPath.get(relatedPath).SPDXID,
      })),
    ),
    ...pythonPackages.map((item) => ({
      spdxElementId: rootPackages[2].SPDXID,
      relationshipType: "DEPENDS_ON",
      relatedSpdxElement: item.SPDXID,
    })),
    ...upstreamPackages.map((item) => ({
      spdxElementId: rootPackages[0].SPDXID,
      relationshipType: "OTHER",
      relatedSpdxElement: item.SPDXID,
      comment:
        "Runtime management or SQL integration; the service remains a separate process and image.",
    })),
    {
      spdxElementId: rootPackages[2].SPDXID,
      relationshipType: "DEPENDS_ON",
      relatedSpdxElement: "SPDXRef-Upstream-AgentGuard",
      comment:
        "The repository-local Python SDK imports the separately installed pinned AgentGuard package; AgentGuard source is not copied into the SDK.",
    },
  ],
  annotations: [
    {
      annotationDate: created,
      annotationType: "OTHER",
      annotator: "Tool: AgentsharkX release artifact generator",
      comment: `package-lock.json sha256 ${createHash("sha256").update(lockText).digest("hex")}`,
    },
    {
      annotationDate: created,
      annotationType: "OTHER",
      annotator: "Tool: AgentsharkX release artifact generator",
      comment: `go.mod sha256 ${createHash("sha256").update(goModText).digest("hex")}; go.sum sha256 ${createHash("sha256").update(goSumText).digest("hex")}`,
    },
    {
      annotationDate: created,
      annotationType: "OTHER",
      annotator: "Tool: AgentsharkX release artifact generator",
      comment:
        "Go module membership is the reviewed linux/amd64 runtime graph for ./cmd/agentshark and ./cmd/agentshark-collector; test-only module dependencies are excluded.",
    },
    {
      annotationDate: created,
      annotationType: "OTHER",
      annotator: "Tool: AgentsharkX release artifact generator",
      comment: `Python SDK pyproject.toml sha256 ${createHash("sha256").update(pythonProjectText).digest("hex")}; constraints.txt sha256 ${createHash("sha256").update(pythonConstraintsText).digest("hex")}`,
    },
  ],
};

const goLicenseRows = goRuntimeDefinitions
  .map(
    (dependency) =>
      `| \`${escapeCell(dependency.path)}\` | \`${escapeCell(dependency.version)}\` | ${dependency.direct ? "runtime (direct)" : "runtime (transitive)"} | ${escapeCell(dependency.license)} |`,
  )
  .join("\n");
const pythonLicenseRows = pythonRuntimeDefinitions
  .map(
    (dependency) =>
      `| \`${escapeCell(dependency.name)}\` | \`${escapeCell(dependency.version)}\` | ${dependency.direct ? "runtime (direct)" : "runtime (transitive)"} | ${escapeCell(dependency.license)} |`,
  )
  .join("\n");
const licenseRows = dependencies
  .map(
    (dependency) =>
      `| \`${escapeCell(dependency.name)}\` | \`${escapeCell(dependency.version)}\` | ${escapeCell(dependency.scope)} | ${escapeCell(dependency.license)} |`,
  )
  .join("\n");
const licenseDocument = `# Dependency license inventory

Generated from the exact npm lockfile, reviewed Go runtime module graph, and pinned local Python SDK resolution used by the \`${releaseVersion}\` preview. Go module versions are checked against \`apps/server/go.mod\`, and their content hashes are recorded from \`apps/server/go.sum\`. Python versions are checked against \`sdk/python/constraints.txt\`; runtime package licenses were reviewed from installed distribution metadata. Generation performs no network access. npm licenses are declarations from the lockfile; \`NOASSERTION\` entries require manual review before redistribution.

Runtime services are separate processes: agentgateway is Apache-2.0, AgentGuard at full source revision \`${agentGuardRevision}\` is GPL-3.0-only, and PostgreSQL uses the PostgreSQL license. Their source and image obligations remain independent from the AgentsharkX Apache-2.0 image. The repository-local Python SDK imports a separately installed pinned AgentGuard checkout for local agent integration; it does not copy AgentGuard source or publish a combined package. Distribution requires a separate license review.

## Go runtime modules

| Package | Version | Scope | Declared license |
| --- | --- | --- | --- |
${goLicenseRows}

## Python SDK runtime packages

| Package | Version | Scope | Declared license |
| --- | --- | --- | --- |
${pythonLicenseRows}

## npm packages

| Package | Version | Scope | Declared license |
| --- | --- | --- | --- |
${licenseRows}
`;

await mkdir(releaseDir, { recursive: true });
await writeFile(
  resolve(releaseDir, "sbom.spdx.json"),
  `${JSON.stringify(document, null, 2)}\n`,
);
await writeFile(resolve(releaseDir, "dependency-licenses.md"), licenseDocument);
console.log(
  `release artifacts: ${document.packages.length} packages, ${dependencies.length} npm dependencies, ${goRuntimeDefinitions.length} Go runtime modules, ${pythonRuntimeDefinitions.length} Python runtime packages`,
);

function packageEntry(name, version, license, primaryPackagePurpose) {
  return {
    name,
    SPDXID: spdxID(`${name}-${version}`),
    versionInfo: version,
    downloadLocation: "NOASSERTION",
    filesAnalyzed: false,
    licenseConcluded: "NOASSERTION",
    licenseDeclared: license,
    copyrightText: "NOASSERTION",
    primaryPackagePurpose,
  };
}

function spdxID(value) {
  return `SPDXRef-${value.replace(/[^A-Za-z0-9.-]+/g, "-")}`;
}

function normalizeLicense(value) {
  if (typeof value === "string" && value.trim()) return value.trim();
  if (value && typeof value === "object" && typeof value.type === "string")
    return value.type;
  return "NOASSERTION";
}

function npmPackageName(location) {
  const marker = "node_modules/";
  const name = location.slice(location.lastIndexOf(marker) + marker.length);
  const parts = name.split("/");
  const valid = name.startsWith("@") ? parts.length === 2 : parts.length === 1;
  if (!valid || parts.some((part) => !part)) {
    throw new Error(
      `cannot derive an npm package name from lockfile location ${location}`,
    );
  }
  return name;
}

function npmPURL(name, version) {
  if (name.startsWith("@")) {
    const [scope, packageName] = name.split("/");
    return `pkg:npm/${encodeURIComponent(scope)}/${encodeURIComponent(packageName)}@${encodeURIComponent(version)}`;
  }
  return `pkg:npm/${encodeURIComponent(name)}@${encodeURIComponent(version)}`;
}

function parseGoRequirements(text) {
  const requirements = new Map();
  let inBlock = false;
  for (const rawLine of text.split("\n")) {
    const line = rawLine.replace(/\/\/.*$/, "").trim();
    if (line === "require (") {
      inBlock = true;
      continue;
    }
    if (inBlock && line === ")") {
      inBlock = false;
      continue;
    }
    const candidate = inBlock
      ? line
      : line.startsWith("require ")
        ? line.slice("require ".length).trim()
        : "";
    const match = candidate.match(/^(\S+)\s+(v\S+)$/);
    if (match) requirements.set(match[1], match[2]);
  }
  return requirements;
}

function parseGoSums(text) {
  const sums = new Map();
  for (const line of text.split("\n")) {
    const match = line.match(/^(\S+)\s+(v\S+)\s+(h1:\S+)$/);
    if (!match || match[2].endsWith("/go.mod")) continue;
    const versions = sums.get(match[1]) ?? new Map();
    versions.set(match[2], match[3]);
    sums.set(match[1], versions);
  }
  return sums;
}

function validateGoRuntimeManifest(definitions, requirements, sums) {
  const classifiedPaths = new Set(definitions.map((item) => item.path));
  const unclassified = [...requirements.keys()].filter(
    (path) => !classifiedPaths.has(path),
  );
  if (unclassified.length > 0) {
    throw new Error(
      `classify new go.mod requirements before generating release artifacts: ${unclassified.join(", ")}`,
    );
  }
  for (const dependency of definitions) {
    const version = requirements.get(dependency.path);
    if (version !== dependency.version) {
      throw new Error(
        `review ${dependency.path} metadata: go.mod has ${version ?? "no requirement"}, manifest expects ${dependency.version}`,
      );
    }
    if (!sums.get(dependency.path)?.has(dependency.version)) {
      throw new Error(
        `missing go.sum content hash for ${dependency.path} ${dependency.version}`,
      );
    }
    for (const relatedPath of dependency.dependencies ?? []) {
      if (!classifiedPaths.has(relatedPath)) {
        throw new Error(
          `unclassified runtime relationship ${dependency.path} -> ${relatedPath}`,
        );
      }
    }
  }
}

function parsePythonConstraints(text) {
  const constraints = new Map();
  for (const rawLine of text.split("\n")) {
    const line = rawLine.replace(/\s+#.*$/, "").trim();
    if (!line || line.startsWith("#")) continue;
    const match = line.match(/^([A-Za-z0-9_.-]+)==([^\s]+)$/);
    if (!match) {
      throw new Error(`unsupported Python constraint: ${line}`);
    }
    const name = match[1].toLowerCase().replaceAll("_", "-");
    if (constraints.has(name)) {
      throw new Error(`duplicate Python constraint: ${name}`);
    }
    constraints.set(name, match[2]);
  }
  return constraints;
}

function validatePythonRuntimeManifest(definitions, constraints, projectText) {
  for (const dependency of definitions) {
    const version = constraints.get(dependency.name);
    if (version !== dependency.version) {
      throw new Error(
        `review ${dependency.name} metadata: constraints have ${version ?? "no version"}, manifest expects ${dependency.version}`,
      );
    }
    if (
      dependency.direct &&
      !projectText.includes(`"${dependency.name}==${dependency.version}"`)
    ) {
      throw new Error(
        `direct Python dependency ${dependency.name} must match pyproject.toml`,
      );
    }
  }
}

function escapeCell(value) {
  return String(value).replaceAll("|", "\\|");
}
