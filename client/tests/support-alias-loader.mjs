// Minimal ESM resolve hook so tests can import real TypeScript sources that use
// the project's "@/*" -> "./src/*" path alias (see client/tsconfig.json).
// Node's built-in type-stripping loader already handles ".ts" syntax; this hook
// only teaches module resolution about the "@/" alias used by src files.
import { fileURLToPath, pathToFileURL } from "node:url";
import path from "node:path";
import fs from "node:fs";

const TESTS_DIR = path.dirname(fileURLToPath(import.meta.url));
const SRC_DIR = path.resolve(TESTS_DIR, "..", "src");

export async function resolve(specifier, context, nextResolve) {
  if (specifier.startsWith("@/")) {
    const rel = specifier.slice(2);
    const candidates = [
      path.resolve(SRC_DIR, `${rel}.ts`),
      path.resolve(SRC_DIR, `${rel}.tsx`),
      path.resolve(SRC_DIR, rel, "index.ts"),
    ];
    for (const candidate of candidates) {
      if (fs.existsSync(candidate)) {
        return { url: pathToFileURL(candidate).href, shortCircuit: true };
      }
    }
  }
  return nextResolve(specifier, context);
}

// `import.meta.env` is a Vite build-time global that does not exist under plain
// Node. Real src modules (e.g. src/lib/api-base.ts) reference it at module top
// level, so loading them here would throw before any test code runs. Shim it
// by rewriting the already-transpiled source to read from a test-controlled
// global instead; tests set `globalThis.__TEST_IMPORT_META_ENV__` as needed.
export async function load(url, context, nextLoad) {
  const result = await nextLoad(url, context);
  const src = typeof result.source === "string" ? result.source : Buffer.isBuffer(result.source) || result.source instanceof Uint8Array
    ? Buffer.from(result.source).toString("utf8")
    : result.source;
  if (typeof src === "string" && src.includes("import.meta.env")) {
    const patched = src.replaceAll(
      "import.meta.env",
      "(globalThis.__TEST_IMPORT_META_ENV__ ?? {})",
    );
    return { ...result, source: patched };
  }
  return result;
}
