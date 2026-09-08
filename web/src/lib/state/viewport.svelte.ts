/**
 * Which of the guidelines' width ranges the window is in, as a rune.
 *
 * Nearly every responsive rule in this UI belongs in a media query and stays
 * there. This exists for the one thing CSS cannot do: the phone's navigation is
 * a different *component* rather than the same one restyled -- a bar of four
 * labelled sections and a menu, not a collapsible sidebar -- so the markup, and
 * not only the presentation, has to know. Reading it here rather than in each
 * component keeps one listener for the whole app and one place where the
 * threshold is written.
 *
 * The first value is read synchronously, so a phone never paints the desktop
 * shell for a frame before correcting itself.
 */

/** `--z-bp-md`, the phone threshold. See docs/ui-guidelines.md. */
const PHONE = '(max-width: 768px)';

class Viewport {
  #phone = $state(false);

  constructor() {
    // Absent in a non-browser build, and jsdom's is not complete; neither is
    // worth a crash on boot, and false is the desktop shell, which is the
    // safer thing to be wrong about.
    if (typeof matchMedia !== 'function') return;
    try {
      const query = matchMedia(PHONE);
      this.#phone = query.matches;
      query.addEventListener('change', (event) => (this.#phone = event.matches));
    } catch {
      /* as above */
    }
  }

  /** True below 768px: the phone layout. */
  get phone(): boolean {
    return this.#phone;
  }
}

export const viewport = new Viewport();
