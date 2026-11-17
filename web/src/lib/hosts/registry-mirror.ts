/**
 * What a host card says about the host's registry mirror.
 *
 * The mirror keeps the public Docker Hub images the host's docker-in-docker
 * sidecars pull, and it is off unless an operator turns it on, so most hosts have
 * none and say nothing. A host that has one says that it is on, or what is wrong
 * with it in the controller's own sentences: they come from the server so that
 * the page keeps no copy that could drift from the problem the same fault raises.
 *
 * Only what changes rarely is on the host. The figures move with every pull and
 * are the metrics' business, which is why there is no hit rate here.
 */
import type { HostRegistryMirror } from '$lib/api/types';

export interface RegistryMirrorLine {
  /** `problem` is a warning, never an error: a broken mirror slows a pull and fails nothing. */
  state: 'ok' | 'problem';
  headline: string;
  /** What is wrong, as the controller says it. Present only with a problem. */
  detail?: string;
  /** What to do about it, as the controller says it. Present only with a problem. */
  fix?: string;
  /** The first thing a reader of a warning needs: that no job is failing. Present only with a problem. */
  consequence?: string;
  /** Facts about the host that are not faults, each one a sentence. */
  notes: string[];
}

const CONSEQUENCE =
  'Nothing fails: the docker-in-docker sidecars on this host pull from Docker Hub as they did before, only slower and against its pull limit.';

const CUSTOM_IMAGE =
  "This host's own sidecar image (agent.dind_image) is not the default, so pools that follow the host's image are not given the mirror.";

/** For a problem the controller sent no sentence for, so the line never says "not working" and stops. */
const GENERAL = 'The agent reports a problem with its registry mirror.';

/** The line for a host's mirror, or null for a host that has none and says nothing. */
export function registryMirrorLine(
  mirror: HostRegistryMirror | null | undefined,
): RegistryMirrorLine | null {
  if (!mirror) return null;
  const notes = mirror.host_image_custom ? [CUSTOM_IMAGE] : [];
  if (!mirror.problem) return { state: 'ok', headline: 'On', notes };
  const line: RegistryMirrorLine = {
    state: 'problem',
    headline: 'Not working',
    detail: mirror.detail || GENERAL,
    consequence: CONSEQUENCE,
    notes,
  };
  if (mirror.fix) line.fix = mirror.fix;
  return line;
}
