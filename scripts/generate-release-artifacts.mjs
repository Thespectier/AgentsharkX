import { createHash } from "node:crypto";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const lockPath = resolve(root, "apps/web/package-lock.json");
const versionsPath = resolve(root, "deploy/versions.env");
const releaseDir = resolve(root, "docs/release");
const lockText = await readFile(lockPath, "utf8");
const lock = JSON.parse(lockText);
const versions = Object.fromEntries(
  (await readFile(versionsPath, "utf8"))
    .split("\n")
    .filter((line) => line && !line.startsWith("#") && line.includes("="))
    .map((line) => {
      const separator = line.indexOf("=");
      return [line.slice(0, separator), line.slice(separator + 1)];
    }),
);

const dependencies = [];
for (const [location, metadata] of Object.entries(lock.packages ?? {})) {
  if (!location.startsWith("node_modules/") || !metadata.version) continue;
  const name = location.slice("node_modules/".length);
  dependencies.push({
    name,
    version: metadata.version,
    license: normalizeLicense(metadata.license),
    scope: metadata.dev ? "build" : "runtime",
    resolved: metadata.resolved,
    integrity: metadata.integrity,
  });
}
dependencies.sort((left, right) => left.name.localeCompare(right.name));

const releaseVersion = versions.AGENTSHARK_VERSION ?? "0.7.0-preview";
const created = "2026-07-22T00:00:00Z";
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
      referenceLocator: `pkg:npm/${encodeURIComponent(dependency.name).replace("%40", "@")}@${dependency.version}`,
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
      versions.AGENTGUARD_VERSION ?? "NOASSERTION",
      "GPL-3.0-only",
      "APPLICATION",
    ),
    SPDXID: "SPDXRef-Upstream-AgentGuard",
    downloadLocation: "https://github.com/WhitzardAgent/AgentGuard",
    supplier: "Organization: WhitzardAgent",
  },
];

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
  packages: [...rootPackages, ...npmPackages, ...upstreamPackages],
  relationships: [
    ...npmPackages.map((item) => ({
      spdxElementId: rootPackages[1].SPDXID,
      relationshipType: "DEPENDS_ON",
      relatedSpdxElement: item.SPDXID,
    })),
    ...upstreamPackages.map((item) => ({
      spdxElementId: rootPackages[0].SPDXID,
      relationshipType: "OTHER",
      relatedSpdxElement: item.SPDXID,
      comment:
        "Runtime HTTP integration; the upstream remains a separate process and image.",
    })),
  ],
  annotations: [
    {
      annotationDate: created,
      annotationType: "OTHER",
      annotator: "Tool: AgentsharkX release artifact generator",
      comment: `package-lock.json sha256 ${createHash("sha256").update(lockText).digest("hex")}`,
    },
  ],
};

const licenseRows = dependencies
  .map(
    (dependency) =>
      `| \`${escapeCell(dependency.name)}\` | \`${escapeCell(dependency.version)}\` | ${escapeCell(dependency.scope)} | ${escapeCell(dependency.license)} |`,
  )
  .join("\n");
const licenseDocument = `# Dependency license inventory

Generated from the exact npm lockfile used by the \`${releaseVersion}\` preview. Go runtime code currently uses only the standard library. Licenses are declarations from the lockfile; \`NOASSERTION\` entries require manual review before redistribution.

Upstream services are separate processes: agentgateway is Apache-2.0 and AgentGuard is GPL-3.0-only. Their source and image obligations remain independent from the AgentsharkX Apache-2.0 image.

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
  `release artifacts: ${document.packages.length} packages, ${dependencies.length} npm dependencies`,
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

function escapeCell(value) {
  return String(value).replaceAll("|", "\\|");
}
