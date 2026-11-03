/**
 * What this browser's panel layouts become once the account's preference
 * document has been read. Pure, so the rule is held by a unit test: the
 * account's layouts win where it has them; where it has none, a browser that
 * any account has used before clears its copy, because the document carries
 * no `panels` at the default and "none" on such a browser is what a reset
 * made elsewhere looks like; only a browser no account has used keeps its
 * copy, which the first save then carries.
 */
import type { PanelLayout } from '../overview/panels';
import type { PanelPage } from './panel-pages';

export type PanelLayouts = Partial<Record<PanelPage, PanelLayout>>;

export function panelLayoutsAfterSync(
  current: PanelLayouts,
  remote: PanelLayouts | undefined,
  previousAccount: string | undefined,
  userID: string,
): PanelLayouts {
  if (remote) return remote;
  if (previousAccount !== undefined) return {};
  void userID;
  return current;
}
