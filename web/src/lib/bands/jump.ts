import { tick } from 'svelte';

/**
 * Bring the evidence for a band figure into view, and put the keyboard there.
 *
 * A figure and the list it counts are on one page, so pressing it is a jump
 * and not a navigation. The hash is replaced rather than pushed: a place on
 * the page should not fill the back button, and the page's own filters are
 * already history entries when they mean something.
 */
export function jumpToSection(id: string): void {
  const target = document.getElementById(id);
  if (!target) return;
  const calm = matchMedia('(prefers-reduced-motion: reduce)').matches;
  target.scrollIntoView({ behavior: calm ? 'auto' : 'smooth', block: 'start' });
  if (!target.hasAttribute('tabindex')) target.setAttribute('tabindex', '-1');
  target.focus({ preventScroll: true });
  if (location.hash !== `#${id}`) {
    history.replaceState(history.state, '', `${location.pathname}${location.search}#${id}`);
  }
}

/**
 * A section that a link from another page may name.
 *
 * The route changes before this node exists, so the browser's own fragment
 * jump has nothing to land on. The action waits until the section is mounted,
 * then does what an in-page figure does. It also follows a hash that changes
 * while the page is open, which is how a browser's back button returns to a
 * section on a page it has already shown.
 */
export function revealHash(node: HTMLElement) {
  const reveal = (): void => {
    if (!node.id || location.hash !== `#${node.id}`) return;
    void tick().then(() => jumpToSection(node.id));
  };
  reveal();
  addEventListener('hashchange', reveal);
  return {
    destroy(): void {
      removeEventListener('hashchange', reveal);
    },
  };
}
