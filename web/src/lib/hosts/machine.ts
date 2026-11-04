/**
 * The machine the controller itself runs on, as the figures a card says.
 *
 * A controller whose agents are all elsewhere has no host of its own, so none of
 * the host cards describe the machine it runs on and nothing on the page said how
 * that machine was doing. The controller samples it on its own tick and sends it
 * on the stream (`controller.machine`); this is what the page makes of it, kept
 * apart from the page so its two decisions are tested without a browser: when
 * there is anything to show, and which figures are worth a line.
 */
import type { Schemas } from '$lib/api/types';
import { NO_VALUE, formatMegabytes, formatNumber } from '$lib/format';
import { stealShown } from './vitals';

export type ControllerMachine = Schemas['ControllerMachine'];

export interface MachineFigure {
  key: 'cpu' | 'controller' | 'steal' | 'load' | 'memory';
  label: string;
  value: string;
  detail: string;
}

/** One decimal at most: a share of 3.75 is a fact, 3.7500000001 is not. */
function round(n: number): string {
  return formatNumber(Math.round(n * 10) / 10);
}

/**
 * Whether the page has a card to draw. Not before the controller has a figure to
 * say (it needs two samples to take a share of an interval), and not when the
 * controller has an agent of its own: that machine is a host, and its card
 * already says all of this, so a second one would count it twice.
 */
export function machineCardShown(
  hosts: readonly { embedded?: boolean }[],
  view: ControllerMachine | null,
): boolean {
  return view !== null && view.usage !== undefined && !hosts.some((h) => h.embedded === true);
}

/**
 * The figures, in the order a person reads a machine: how busy, how much of that
 * was the controller's own doing, how much the hypervisor kept, how far behind its
 * work is, and how much room is left. A figure the controller did not measure is
 * left out and not drawn as zero, and steal is said only once it is more than
 * noise, as a host card says it.
 *
 * The controller's own share is said even when it is small, because a small one is
 * the answer: a machine at 70% with the controller at 0.1% is busy with something
 * else, and a card that left the figure out for being low would hide that. A
 * controller too old to send it leaves it out, as one that did not measure steal
 * leaves that out.
 */
export function machineFigures(view: ControllerMachine): MachineFigure[] {
  const u = view.usage;
  if (!u) return [];
  const out: MachineFigure[] = [];
  if (u.cpu_percent !== undefined) {
    out.push({
      key: 'cpu',
      label: 'CPU busy',
      value: `${round(u.cpu_percent)}%`,
      detail: 'Counts time waiting on a disk and time a hypervisor kept as busy',
    });
  }
  if (u.process_cpu_percent !== undefined) {
    out.push({
      key: 'controller',
      label: 'Controller',
      value: `${round(u.process_cpu_percent)}%`,
      detail: "Of the machine's CPU, used by the controller process itself",
    });
  }
  const steal = stealShown(u.steal_percent);
  if (steal !== null) {
    out.push({
      key: 'steal',
      label: 'Steal',
      value: `${round(steal)}%`,
      detail: 'Kept from this virtual machine by its hypervisor, so not its own work',
    });
  }
  if (u.load_average_1m !== undefined) {
    out.push({
      key: 'load',
      label: 'Load',
      value: round(u.load_average_1m),
      detail: view.cpus
        ? `One-minute average, on ${formatNumber(view.cpus)} CPUs`
        : 'One-minute average',
    });
  }
  if (u.memory_available_mb !== undefined) {
    out.push({
      key: 'memory',
      label: 'Memory available',
      value: formatMegabytes(u.memory_available_mb),
      detail: view.memory_mb ? `Of ${formatMegabytes(view.memory_mb)}` : NO_VALUE,
    });
  }
  return out;
}
