// Lets the unit tests import the app's modules as the app does.
//
// Vite resolves `import { x } from '../format'` to format.ts; Node's module
// loader does not, and would otherwise confine the unit tests to modules that
// import nothing. The hook tries the `.ts` spelling of any extensionless
// relative import first, and falls back to Node's own resolution for anything
// else, so a bare package import is untouched.
import { register } from 'node:module';

register('./resolve.mjs', import.meta.url);
