import { readFileSync } from "node:fs";
import { registerHooks } from "node:module";
import { transformSync } from "rolldown/utils";

registerHooks({
  resolve(specifier, context, next) {
    try { return next(specifier, context); }
    catch (error) {
      if (error.code !== "ERR_MODULE_NOT_FOUND" || !specifier.startsWith(".")) throw error;
      for (const suffix of [".ts", ".tsx"]) {
        try { return next(specifier + suffix, context); } catch {}
      }
      throw error;
    }
  },
  load(url, context, next) {
    if (!url.startsWith("file:") || !/\.(tsx?|jsx)$/.test(url)) return next(url, context);
    const result = transformSync(new URL(url).pathname, readFileSync(new URL(url), "utf8"), { jsx: { runtime: "automatic" } });
    if (result.errors.length) throw new Error(JSON.stringify(result.errors));
    const source = result.code;
    return { format: "module", source, shortCircuit: true };
  },
});
