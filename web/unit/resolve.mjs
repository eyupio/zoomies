import { fileURLToPath, pathToFileURL } from 'node:url';
import { dirname, resolve as resolvePath } from 'node:path';

// Vite's `$lib` alias, which the unit runner does not otherwise know about.
// Without it a module can only be unit-tested if it happens to import nothing
// across the alias -- which is not a property worth choosing test coverage by.
const LIB = pathToFileURL(
  resolvePath(dirname(fileURLToPath(import.meta.url)), '../src/lib') + '/',
).href;

export async function resolve(specifier, context, next) {
  if (specifier === '$lib' || specifier.startsWith('$lib/')) {
    const rest = specifier === '$lib' ? '' : specifier.slice('$lib/'.length);
    const url = LIB + rest;
    // The same extensionless-TypeScript retry the relative branch below does.
    if (!/\.[a-z]+$/i.test(url)) {
      try {
        return await next(`${url}.ts`, context);
      } catch {
        // Not a TypeScript module after all; fall through.
      }
    }
    return next(url, context);
  }
  if (specifier.startsWith('.') && !/\.[a-z]+$/i.test(specifier)) {
    try {
      return await next(`${specifier}.ts`, context);
    } catch {
      // Not a TypeScript module after all; let Node say what it is.
    }
  }
  return next(specifier, context);
}
