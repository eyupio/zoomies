/**
 * Light, dark or whatever the operating system says.
 *
 * `dark` is the default. `system` writes no attribute, so `prefers-color-scheme`
 * decides. Every choice writes `data-theme` (bar `system`) and persists. The
 * inline script in index.html applies the stored value before first paint; this
 * module keeps subsequent choices in sync.
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

/**
 * The three choices as a segmented control offers them: the Appearance page
 * and the phone's menu both draw one, and the words are the same in each.
 */
export const THEME_OPTIONS: readonly { value: ThemeChoice; label: string; name: string }[] = [
  { value: 'system', label: 'System', name: 'Follow the operating system' },
  { value: 'light', label: 'Light', name: 'Always light, whatever the system says' },
  { value: 'dark', label: 'Dark', name: 'Always dark' },
];

const KEY = 'zoomies.theme';

/**
 * Point the browser chrome at the theme actually on screen.
 *
 * `system` hands the tags back their media queries so the operating system
 * keeps switching them with no JavaScript involved; an explicit choice pins one
 * tag on and the other off. `not all` is the standard way to say "never match".
 */
export function syncBrowserChrome(choice: ThemeChoice): void {
  const style =
    document.documentElement.getAttribute('data-ui-style') === 'mission-control'
      ? 'mission-control'
      : 'classic';
  for (const which of ['light', 'dark'] as const) {
    const tag = document.querySelector(`meta[data-theme-colour="${which}"]`);
    if (!tag) continue;
    const colour = tag.getAttribute(`data-${style}-colour`);
    if (colour) tag.setAttribute('content', colour);
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

/** Dark is the default; following the system is a choice that has to be stored. */
function stored(): ThemeChoice {
  const value = storage.get(KEY);
  return value === 'light' || value === 'system' ? value : 'dark';
}

class Theme {
  #choice = $state<ThemeChoice>('system');
  #systemDark = $state(false);

  constructor() {
    this.#choice = stored();
    // Bootstrap and later choices use the same meta colours, including visual style.
    if (typeof document !== 'undefined') syncBrowserChrome(this.#choice);
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
    if (choice === 'system') document.documentElement.removeAttribute('data-theme');
    else document.documentElement.setAttribute('data-theme', choice);
    storage.set(KEY, choice);
    syncBrowserChrome(choice);
  }

  /** light → dark → system → light. What the top bar's toggle does. */
  cycle(): void {
    this.set(this.#choice === 'light' ? 'dark' : this.#choice === 'dark' ? 'system' : 'light');
  }
}

export const theme = new Theme();
