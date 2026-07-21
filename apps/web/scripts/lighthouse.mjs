import { spawn } from "node:child_process";
import { mkdir, writeFile } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { chromium } from "playwright";
import { launch } from "chrome-launcher";
import lighthouse from "lighthouse";

const here = dirname(fileURLToPath(import.meta.url));
const webRoot = resolve(here, "..");
const reportPath = resolve(webRoot, "../../docs/screenshots/lighthouse-accessibility.json");
const target = "http://127.0.0.1:4173/";

async function runBuild() {
  const build = spawn("npm", ["run", "build"], { cwd: webRoot, stdio: "inherit" });
  const code = await new Promise((resolveExit) => build.once("exit", resolveExit));
  if (code !== 0) throw new Error(`Production build failed with exit code ${code}`);
}

async function isReady() {
  try {
    return (await fetch(target)).ok;
  } catch {
    return false;
  }
}

async function waitForPreview() {
  for (let attempt = 0; attempt < 80; attempt += 1) {
    if (await isReady()) return;
    await new Promise((resolveWait) => setTimeout(resolveWait, 250));
  }
  throw new Error("Vite preview did not become ready on port 4173");
}

await runBuild();

let preview;
if (!(await isReady())) {
  preview = spawn(resolve(webRoot, "node_modules/.bin/vite"), ["preview", "--host", "127.0.0.1"], {
    cwd: webRoot,
    stdio: ["ignore", "pipe", "pipe"],
  });
  await waitForPreview();
}

const chrome = await launch({
  chromePath: chromium.executablePath(),
  chromeFlags: ["--headless=new", "--no-sandbox", "--disable-dev-shm-usage"],
});

try {
  const result = await lighthouse(target, {
    logLevel: "error",
    onlyCategories: ["accessibility"],
    output: "json",
    port: chrome.port,
  });
  if (!result) throw new Error("Lighthouse returned no result");
  const score = result.lhr.categories.accessibility.score ?? 0;
  const summary = {
    capturedAt: new Date().toISOString(),
    url: target,
    lighthouseVersion: result.lhr.lighthouseVersion,
    accessibilityScore: Math.round(score * 100),
    audits: Object.fromEntries(
      Object.entries(result.lhr.audits)
        .filter(([, audit]) => audit.score !== null && audit.score < 1)
        .map(([id, audit]) => [
          id,
          {
            score: audit.score,
            title: audit.title,
            items:
              audit.details?.type === "table"
                ? audit.details.items.map((item) => ({
                    selector: item.node?.selector,
                    snippet: item.node?.snippet,
                    explanation: item.node?.explanation,
                  }))
                : undefined,
          },
        ]),
    ),
  };
  await mkdir(dirname(reportPath), { recursive: true });
  await writeFile(reportPath, `${JSON.stringify(summary, null, 2)}\n`);
  console.log(`Lighthouse accessibility: ${summary.accessibilityScore}/100`);
  if (score < 0.95) process.exitCode = 1;
} finally {
  await chrome.kill();
  preview?.kill("SIGTERM");
}
