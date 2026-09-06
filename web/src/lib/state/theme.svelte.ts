/**
 * Light, dark or whatever the operating system says.
 *
 * `system` is the default and writes no attribute, so `prefers-color-scheme`
 * decides. An explicit choice writes `data-theme` on `<html>` and persists. The
 * inline script in index.html applies the stored value before first paint; this
 * module only keeps it in sync afterwards -- it deliberately does not repeat
 * that work.
 *
 * The one thing an attribute on `<html>` cannot reach is the browser's own
 * chrome, which reads `<meta name="theme-color">`. The two tags in index.html
 * are media-scoped so the "system" default needs no JavaScript at all; an
 * explicit choice pins them here, because otherwise an operator on a dark
 * laptop who asks for the light theme still gets a black bar above a white
 * page.
 */
import { storage } from './prefs.svelte';

export type ThemeChoice = 'light' | 'dark' | 'system';
export type ResolvedTheme = 'light' | 'dark';

const KEY = 'zoomies.theme';

/**
 * Point the browser chrome at the theme actually on screen.
 *
 * `system` hands the tags back their media queries so the operating system
 * keeps switching them with no JavaScript involved; an explicit choice pins one
 * tag on and the other off. `not all` is the standard way to say "never match".
 */
function pinBrowserChrome(choice: ThemeChoice): void {
  for (const which of ['light', 'dark'] as const) {
    const tag = document.querySelector(`meta[data-theme-colour="${which}"]`);
    if (!tag) continue;
    tag.setAttribute(
      'media',
      choice === 'system'
        ? `(prefers-color-scheme: ${which})`
        : choice === which
          ? 'all'
          : 'not all',
    );
  }
}

function stored(): ThemeChoice {
  const value = storage.get(KEY);
  return value === 'light' || value === 'dark' ? value : 'system';
}

class Theme {
  #choice = $state<ThemeChoice>('system');
  #systemDark = $state(false);

  constructor() {
    this.#choice = stored();
    // The stored choice was applied to <html> before first paint by the inline
    // script, which cannot reach the meta tags; catch them up here.
    if (typeof document !== 'undefined') pinBrowserChrome(this.#choice);
    if (typeof window !== 'undefined' && typeof window.matchMedia === 'function') {
      const query = window.matchMedia('(prefers-color-scheme: dark)');
      this.#systemDark = query.matches;
      query.addEventListener('change', (e) => {
        this.#systemDark = e.matches;
      });
    }
  }

  /** What the operator chose, including `system`. */
  get choice(): ThemeChoice {
    return this.#choice;
  }

  /** What is actually on screen. */
  get resolved(): ResolvedTheme {
    if (this.#choice === 'system') return this.#systemDark ? 'dark' : 'light';
    return this.#choice;
  }

  set(choice: ThemeChoice): void {
    this.#choice = choice;
    if (typeof document === 'undefined') return;
    if (choice === 'system') {
      document.documentElement.removeAttribute('data-theme');
      storage.remove(KEY);
    } else {
      document.documentElement.setAttribute('data-theme', choice);
      storage.set(KEY, choice);
    }
    pinBrowserChrome(choice);
  }

  /** light → dark → system → light. What the top bar's toggle does. */
  cycle(): void {
    this.set(this.#choice === 'light' ? 'dark' : this.#choice === 'dark' ? 'system' : 'light');
  }
}

export const theme = new Theme();
