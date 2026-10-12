import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const webDir = path.resolve(scriptDir, "..");
const sourceDir = path.join(webDir, "src");

function maskCommentsAndStrings(source) {
  const chars = [...source];
  let i = 0;
  let blockDepth = 0;
  let stringMode = "code";

  const blank = (index) => {
    if (chars[index] !== "\n" && chars[index] !== "\r") chars[index] = " ";
  };

  while (i < chars.length) {
    if (blockDepth > 0) {
      if (chars[i] === "{" && chars[i + 1] === "-") {
        blank(i++);
        blank(i++);
        blockDepth += 1;
      } else if (chars[i] === "-" && chars[i + 1] === "}") {
        blank(i++);
        blank(i++);
        blockDepth -= 1;
      } else {
        blank(i++);
      }
      continue;
    }

    if (stringMode === "triple") {
      if (chars[i] === '"' && chars[i + 1] === '"' && chars[i + 2] === '"') {
        blank(i++);
        blank(i++);
        blank(i++);
        stringMode = "code";
      } else {
        blank(i++);
      }
      continue;
    }

    if (stringMode === "string") {
      if (chars[i] === "\\") {
        blank(i++);
        if (i < chars.length) blank(i++);
      } else if (chars[i] === '"') {
        blank(i++);
        stringMode = "code";
      } else {
        blank(i++);
      }
      continue;
    }

    if (chars[i] === "{" && chars[i + 1] === "-") {
      blank(i++);
      blank(i++);
      blockDepth = 1;
    } else if (chars[i] === "-" && chars[i + 1] === "-") {
      while (i < chars.length && chars[i] !== "\n" && chars[i] !== "\r") blank(i++);
    } else if (chars[i] === '"' && chars[i + 1] === '"' && chars[i + 2] === '"') {
      blank(i++);
      blank(i++);
      blank(i++);
      stringMode = "triple";
    } else if (chars[i] === '"') {
      blank(i++);
      stringMode = "string";
    } else {
      i += 1;
    }
  }

  return chars.join("");
}

function importsIn(source) {
  return maskCommentsAndStrings(source)
    .split(/\r?\n/)
    .flatMap((line, index) => {
      const match = line.match(/^\s*import\s+([A-Z][A-Za-z0-9_.]*)\b/);
      return match ? [{ module: match[1], line: index + 1 }] : [];
    });
}

const forbidden = {
  domain: new Set(["Api", "Feature", "Page", "App", "Http", "Html", "Browser", "File", "Time", "Task", "Process"]),
  api: new Set(["Feature", "Page"]),
  queue: new Set(["Api", "App", "Feature", "Page", "Ui", "Http", "Html", "Browser", "File", "Time", "Task", "Process", "Platform"]),
  view: new Set(["Api", "Http", "File", "Time", "Task", "Browser", "Process", "Dom", "Page", "Platform"]),
};

function rootName(moduleName) {
  return moduleName.split(".")[0];
}

function policyFor(relativePath) {
  const normalized = relativePath.split(path.sep).join("/");
  if (normalized.startsWith("Domain/")) return ["Domain", forbidden.domain];
  if (normalized.startsWith("Api/")) return ["Api", forbidden.api];
  if (normalized === "Feature/UploadQueue/Queue.elm") return ["Feature.UploadQueue.Queue", forbidden.queue];
  if (normalized === "Feature/UploadQueue/View.elm") return ["Feature.UploadQueue.View", forbidden.view];
  return null;
}

function violations(relativePath, source) {
  const policy = policyFor(relativePath);
  if (!policy) return [];
  const [owner, blocked] = policy;
  return importsIn(source)
    .filter(({ module }) => blocked.has(rootName(module)))
    .map(({ module, line }) => `${relativePath}:${line}: ${owner} may not import ${module}`);
}

function runFixtures() {
  const bad = violations("Domain/Probe.elm", "module Domain.Probe exposing (probe)\nimport Http\n");
  if (bad.length !== 1 || !bad[0].includes("Domain may not import Http")) {
    throw new Error("fixture failed: prohibited import was not detected");
  }

  const harmless = `{-
import Http
{- nested comment
import Api.Post
-}
-}
-- import Browser.Dom
"""docs may show this import:
import Feature.Search
"""
module Domain.Probe exposing (probe)
import Domain.Post
`;
  if (violations("Domain/Probe.elm", harmless).length !== 0) {
    throw new Error("fixture failed: comment or string example was treated as an import");
  }

  const queueBad = violations("Feature/UploadQueue/Queue.elm", "module Feature.UploadQueue.Queue exposing (x)\nimport Api.Upload\n");
  if (queueBad.length !== 1 || !queueBad[0].includes("Queue may not import Api.Upload")) {
    throw new Error("fixture failed: queue boundary import was not detected");
  }

  const apiBad = violations("Api/Probe.elm", "module Api.Probe exposing (x)\nimport Feature.Search\n");
  if (apiBad.length !== 1 || !apiBad[0].includes("Api may not import Feature.Search")) {
    throw new Error("fixture failed: API boundary import was not detected");
  }

  const viewBad = violations("Feature/UploadQueue/View.elm", "module Feature.UploadQueue.View exposing (x)\nimport Page.Library\n");
  if (viewBad.length !== 1 || !viewBad[0].includes("View may not import Page.Library")) {
    throw new Error("fixture failed: view boundary import was not detected");
  }

  const viewApiBad = violations("Feature/UploadQueue/View.elm", "module Feature.UploadQueue.View exposing (x)\nimport Api.Upload\n");
  if (viewApiBad.length !== 1 || !viewApiBad[0].includes("View may not import Api.Upload")) {
    throw new Error("fixture failed: view API import was not detected");
  }
}

function elmFiles(directory) {
  return fs.readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const fullPath = path.join(directory, entry.name);
    return entry.isDirectory() ? elmFiles(fullPath) : entry.name.endsWith(".elm") ? [fullPath] : [];
  });
}

try {
  runFixtures();
  const failures = elmFiles(sourceDir).flatMap((file) => {
    const relativePath = path.relative(sourceDir, file);
    return violations(relativePath, fs.readFileSync(file, "utf8"));
  });
  if (failures.length) {
    console.error(failures.join("\n"));
    process.exitCode = 1;
  } else {
    console.log("Frontend import boundaries passed (fixtures included).");
  }
} catch (error) {
  console.error(error.message);
  process.exitCode = 1;
}
