/**
 * What a host reports it can run a job in.
 *
 * The Backends block lists these, and a closed host card has to know whether
 * there is anything in them it may not fold away: an unavailable backend is the
 * reason a host takes no work, and its own explanation is in that block.
 */
import type { BackendInfo, Host } from '$lib/api/types';

/** The rich report where an agent sent one, and its plain list of kinds where that is all it sent. */
export function backendRows(
  info: readonly BackendInfo[] | undefined,
  kinds: readonly string[] | undefined,
): readonly BackendInfo[] {
  if (info && info.length > 0) return info;
  return (kinds ?? []).map((kind) => ({ kind: kind as BackendInfo['kind'] }));
}

/**
 * Whether the host has nothing to place a runner in: no backend reported, or
 * every one of them unavailable. One that works is enough. Docker without
 * Podman is how most hosts are, and the agent words that absence with the
 * commands to install it, so a card that surfaced it would carry the same
 * paragraph on every host in the fleet. An omitted `available` is how an older
 * agent says nothing is wrong, so only an explicit false counts against a row.
 */
export function backendTrouble(host: Pick<Host, 'backend_info' | 'backends'>): boolean {
  const rows = backendRows(host.backend_info, host.backends);
  return rows.length === 0 || rows.every((row) => row.available === false);
}
