import { storage } from './prefs.svelte';
import { syncBrowserChrome, theme } from './theme.svelte';
import { resolveUIStyle, UI_STYLE_KEY, type UIStyle } from './ui-style';

class VisualStyle {
  #choice = $state<UIStyle>(resolveUIStyle(storage.get(UI_STYLE_KEY)));

  get choice(): UIStyle {
    return this.#choice;
  }

  set(value: UIStyle): void {
    const choice = resolveUIStyle(value);
    this.#choice = choice;
    if (typeof document === 'undefined') return;
    // Mission Control is the default, so it stores nothing; Classic is the
    // deliberate choice and has to be remembered.
    if (choice === 'classic') {
      document.documentElement.removeAttribute('data-ui-style');
      storage.set(UI_STYLE_KEY, choice);
    } else {
      document.documentElement.setAttribute('data-ui-style', choice);
      storage.remove(UI_STYLE_KEY);
    }
    // Only root tokens change: editors, the fleet cache and Eli keep their state.
    syncBrowserChrome(theme.choice);
  }

  /**
   * Show a style for this page without choosing it.
   *
   * The component gallery reviews four combinations without touching the
   * operator's saved preference, but components that read this store (a fresh
   * data grid's phone fallback, most visibly) have to see the combination the
   * page is rendering, or the review shows a Classic decision inside Mission
   * Control tokens. The caller owns the document attributes and the address:
   * this only makes the rune agree with them.
   */
  preview(value: UIStyle): void {
    this.#choice = resolveUIStyle(value);
  }
}

export const visualStyle = new VisualStyle();
