/**
 * Entry point for /gallery.html, the development-only component gallery.
 *
 * Its own Vite entry rather than a route of the app: `vite build` never builds
 * this document, so the gallery costs the shell budget nothing and no operator
 * can reach it. It imports the app stylesheet, which is where the tokens and
 * the fonts are, so what it renders is what a page renders.
 *
 * The style and colour mode are applied before the first component mounts. The
 * body is empty until then, so there is nothing to flash. Reading the choice
 * from the query string rather than from `localStorage` means opening the
 * gallery never changes the developer's real Appearance settings, which a
 * review tool must not do.
 */
import { mount } from 'svelte';
import '../app.css';
import { installShortcuts } from '$lib/keys';
import Gallery from './Gallery.svelte';
import { applyChoice, readChoice } from './style';

applyChoice(readChoice(location.search));

// The shell installs this, and the overlays depend on it: Escape is handled
// here and nowhere else, so the gallery must have it for a Drawer or a Dialog
// to close with the keyboard exactly as it does in the product. The actions
// are deliberately empty, because there is no fleet here to refresh, search
// or navigate.
installShortcuts({
  palette: () => {},
  help: () => {},
  search: () => {},
  refresh: () => {},
  eli: () => {},
  focus: () => false,
  go: () => {},
});

const target = document.getElementById('gallery');
if (!target) {
  throw new Error('The #gallery element is missing from gallery.html; the gallery cannot mount.');
}

export default mount(Gallery, { target });
