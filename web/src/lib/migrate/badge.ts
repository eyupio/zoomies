/**
 * The badge, as the review step shows it.
 *
 * This is the browser's half of internal/migrate/badge.go. The two have to
 * agree exactly: the server writes the line into the README, and the review
 * step shows the operator the line it is about to write. A disagreement would
 * show one badge in the preview and commit another.
 */

/** Where the badge is served from. */
export const BADGE_URL = 'https://zoomies.sh/badge.svg';

/** Where clicking it goes. */
export const BADGE_LINK = 'https://zoomies.sh';

/** The alternative text: what a screen reader says, and what a README shows
 * when the image cannot load. */
export const BADGE_ALT = 'CI has the Zoomies';

/** The line itself. */
export const BADGE_MARKDOWN = `[![${BADGE_ALT}](${BADGE_URL})](${BADGE_LINK})`;

/** The sentence the results screen says for each outcome the server reports. */
export function describeBadge(badge: string | undefined, reason: string | undefined): string {
  switch (badge) {
    case 'added':
      return 'The badge is in the README.';
    case 'present':
      return 'The README already had the badge.';
    case 'no_readme':
      return `No badge: ${reason || 'there was no Markdown README to put it in'}.`;
    case 'unread':
      return `No badge: the README could not be read${reason ? ` (${reason})` : ''}.`;
    default:
      return '';
  }
}
