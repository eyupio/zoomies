/**
 * What the side panel says about the controller build this page is talking
 * to, and where that points.
 *
 * Three kinds of build reach an operator, and each is identified differently:
 * a release by its tag, because that is what an upgrade and a bug report name;
 * a build from main by its commit, because every one of them is published under
 * the same moving `dev` tag and the tag alone cannot tell two apart; and a
 * local build, which is neither and is said to be so rather than dressed up as
 * one of them. It is plain enough to test in Node, which is why it lives apart
 * from the component.
 */
import { REPO_URL } from '../links';

export interface BuildMeta {
  version?: string;
  version_channel?: string;
  commit?: string;
}

export interface BuildLabel {
  /** "Release", "Dev" or "Local build". */
  kind: string;
  /** The tag or the short commit. */
  name: string;
  /** Where on GitHub that release or commit is, when it is published there. */
  href?: string;
  /** The full line, for a tooltip and a screen reader. */
  title: string;
}

export function buildLabel(meta: BuildMeta | null | undefined): BuildLabel | null {
  const channel = meta?.version_channel?.trim() ?? '';
  const commit = meta?.commit?.trim() ?? '';
  const short = commit.slice(0, 7);
  // version is "1.3.0 (abc1234)": the part before the commit is the build.
  const version = (meta?.version ?? '').replace(/\s*\([0-9a-f]+\)\s*$/, '').trim();

  if (channel.startsWith('v')) {
    return {
      kind: 'Release',
      name: channel,
      href: `${REPO_URL}/releases/tag/${channel}`,
      title: `Zoomies ${channel}${short ? ` (${short})` : ''}, a published release`,
    };
  }
  if (channel === 'dev') {
    return {
      kind: 'Dev',
      name: short || version || 'dev',
      href: commit ? `${REPO_URL}/commit/${commit}` : undefined,
      title: `Zoomies development build${short ? ` from commit ${short}` : ''}, published under the dev tag`,
    };
  }
  if (!version && !short) return null;
  return {
    kind: 'Local build',
    name: short || version,
    title: `Zoomies ${version || 'local build'}${short ? ` (${short})` : ''}, built locally and not published`,
  };
}
