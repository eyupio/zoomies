/**
 * What each page's band says, from the figures the page already has.
 *
 * Plain functions, as the Overview's status line is, so each sentence can be held to
 * its numbers by a test: a band that said "3 hosts are connected" over a count that
 * included one that had stopped sending heartbeats would be wrong in a way a layout
 * change is very good at hiding. The Runners band lives beside these in
 * runners-line.ts.
 */

export interface BandLine {
  headline: string;
  /** The line drawn in the brand's blue; empty when there is nothing to split. */
  accent: string;
  lede: string;
}

const count = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`;
const is = (n: number) => (n === 1 ? 'is' : 'are');

export function poolsLine(p: {
  enabled: number;
  total: number;
  live: number;
  max: number;
  atCeiling: number;
}): BandLine {
  if (p.total === 0) {
    return {
      headline: 'No pools yet.',
      accent: '',
      lede: 'A pool decides what labels your runners answer to.',
    };
  }
  return {
    headline:
      p.enabled === p.total
        ? `${count(p.total, 'pool', 'pools')} ${is(p.total)} enabled.`
        : `${p.enabled} of ${p.total} pools are enabled.`,
    accent: `${p.live} of ${p.max} runners live.`,
    lede:
      p.atCeiling > 0
        ? `${p.atCeiling} ${is(p.atCeiling)} at its ceiling.`
        : 'None is at its ceiling.',
  };
}

export function queueLine(q: {
  queued: number;
  running: number;
  /** The median wait, already worded, or null when there is not one. */
  medianWait: string | null;
}): BandLine {
  if (q.queued === 0) {
    return {
      headline: 'Nothing is waiting.',
      accent: `${q.running} running.`,
      lede: 'Every job has a runner.',
    };
  }
  return {
    headline: `${count(q.queued, 'job', 'jobs')} ${is(q.queued)} waiting for a runner.`,
    accent: `${q.running} running.`,
    lede: q.medianWait ? `The median wait is ${q.medianWait}.` : 'Jobs start oldest first.',
  };
}

export function jobsLine(j: {
  running: number;
  queued: number;
  succeeded: number;
  failed: number;
  /** The window the finished counts cover, as a phrase: "the last hour". */
  windowText: string;
}): BandLine {
  return {
    headline: `${count(j.running, 'job', 'jobs')} ${is(j.running)} running.`,
    accent: `${j.queued} waiting for a runner.`,
    lede: `${j.failed} failed and ${j.succeeded} succeeded in ${j.windowText}.`,
  };
}

export function workflowsLine(w: { running: number; queued: number }): BandLine {
  const inFlight = w.running + w.queued;
  return {
    headline: `${count(inFlight, 'job', 'jobs')} ${is(inFlight)} in flight.`,
    accent: `${w.running} running, ${w.queued} waiting.`,
    lede: 'Open a run to see its jobs.',
  };
}

export function hostsLine(h: {
  connected: number;
  total: number;
  inUse: number;
  capacity: number;
}): BandLine {
  if (h.total === 0) {
    return {
      headline: 'No hosts yet.',
      accent: '',
      lede: 'Add one to give runners somewhere to start.',
    };
  }
  const silent = h.total - h.connected;
  return {
    headline:
      silent === 0
        ? `${count(h.total, 'host', 'hosts')} ${is(h.total)} connected.`
        : `${h.connected} of ${h.total} hosts are connected.`,
    accent: `${h.inUse} of ${h.capacity} slots in use.`,
    lede:
      silent === 0
        ? 'Every host has checked in.'
        : `${silent} ${silent === 1 ? 'has' : 'have'} not checked in.`,
  };
}

export function providersLine(p: {
  providers: number;
  owned: number;
  ceiling: number;
  paused: number;
  review: number;
}): BandLine {
  if (p.providers === 0) {
    return {
      headline: 'No providers yet.',
      accent: '',
      lede: 'A provider rents machines for pools that need them.',
    };
  }
  return {
    headline: `${count(p.owned, 'machine', 'machines')} ${is(p.owned)} owned.`,
    accent: `Up to ${p.ceiling} at once.`,
    lede:
      p.review > 0
        ? `${p.review} ${p.review === 1 ? 'needs' : 'need'} review.`
        : p.paused > 0
          ? `${count(p.paused, 'provider', 'providers')} ${is(p.paused)} paused.`
          : 'Nothing needs review.',
  };
}

export function installationsLine(i: {
  connections: number;
  healthy: number;
  unhealthy: number;
  unchecked: number;
  pools: number;
  lowQuota: number;
}): BandLine {
  if (i.connections === 0) {
    return {
      headline: 'GitHub is not connected.',
      accent: '',
      lede: 'Connect the App before a pool or a runner can exist.',
    };
  }
  return {
    headline: `${count(i.connections, 'connection', 'connections')} to GitHub.`,
    accent:
      i.unhealthy > 0
        ? `${i.unhealthy} not working.`
        : i.unchecked > 0
          ? `${i.unchecked} not checked yet.`
          : 'Every one answered.',
    lede:
      i.lowQuota > 0
        ? `${count(i.lowQuota, 'App is', 'Apps are')} below 10% of the API quota.`
        : i.pools > 0
          ? `${count(i.pools, 'pool depends', 'pools depend')} on them.`
          : 'No pool depends on them yet.',
  };
}

export function migrateLine(): BandLine {
  return {
    headline: 'Move repositories onto this fleet.',
    accent: 'You see every diff first.',
    lede: 'Rewrite runs-on and open a pull request for each repository.',
  };
}

export function kennelLine(k: {
  enabled: boolean;
  repositories: number;
  notTracked: number;
  attention: number;
  clean: number;
  first: { name: string; findings: string; public: boolean } | null;
  more: number;
}): BandLine {
  if (!k.enabled) {
    return {
      headline: 'Kennel Club is off.',
      accent: '',
      lede: 'Turn it on to check the repositories this fleet serves.',
    };
  }
  if (k.repositories === 0) {
    // A repository somebody chose not to track is a decision, not an absence,
    // and the page's own empty state says which of the two this is.
    if (k.notTracked > 0) {
      return {
        headline: 'Every repository is set aside.',
        accent: '',
        lede: 'Kennel Club has been told not to look at them.',
      };
    }
    return {
      headline: 'No repositories to check yet.',
      accent: '',
      lede: 'Kennel Club checks the repositories this fleet has run jobs for.',
    };
  }
  const clean =
    k.clean > 0 ? `${count(k.clean, 'repository', 'repositories')} with nothing open.` : '';
  if (k.attention === 0) {
    return {
      headline: 'No repository needs attention.',
      accent: clean,
      lede: 'No repository has an error or a warning open.',
    };
  }
  return {
    headline: `${count(k.attention, 'repository needs', 'repositories need')} attention.`,
    accent: clean,
    lede: k.first
      ? `${k.first.name} has ${k.first.findings} open.${
          k.first.public
            ? ' It is public, so a pull request from a stranger can reach this fleet.'
            : ''
        }${
          k.more > 0
            ? ` ${count(k.more, 'more repository needs', 'more repositories need')} attention.`
            : ''
        }`
      : 'Open the repositories to see what is wrong.',
  };
}

/** A window as describeWindow words it ("1 hour", "24 hours") as a period: "the last hour". */
export function windowPhrase(window: string): string {
  return window ? `the last ${window.replace(/^1 /, '')}` : 'the reported window';
}
