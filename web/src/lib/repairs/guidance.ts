import type { Schemas } from '../api/types';

/*
  What to try next. A repair that did not end well says why in the controller's
  words; this adds what a person can do about it, keyed by the failure code the
  worker recorded or, when there is none, by the state the repair ended in. The
  copy is the first draft of the design and a change to a sentence is welcome;
  the mapping from code to advice is what the tests hold.
*/

export interface Guidance {
  /** What happened, in one line. */
  headline: string;
  /** What to do about it, at most three. */
  steps: string[];
}

const UNSUPPORTED_FIX: Guidance = {
  headline: 'Eli could not find a fix it was willing to push',
  steps: [
    'Add a note naming the file or the test, then try again.',
    'If this failure looks unrelated to the pull request, rerun the failed job instead.',
  ],
};

/** What a repair that has not finished says: nothing is wrong, and nothing needs doing. */
const STILL_WORKING: Guidance = {
  headline: 'Eli is still working on it',
  steps: ['You can leave this page. The repair carries on without it.'],
};

const TABLE: Record<string, Guidance> = {
  unsupported_fix: UNSUPPORTED_FIX,
  context_limit: {
    headline: 'The failure and the source were too big for one attempt',
    steps: ['Name the file or the test in a note so Eli reads less, then try again.'],
  },
  source_unavailable: {
    headline: 'Eli could not read the source',
    steps: [
      'Check that the GitHub App has the Contents permission.',
      'Check that the pull request changes fewer than 100 files.',
    ],
  },
  provider_unavailable: {
    headline: 'The model provider failed',
    steps: ['Test it in Eli settings, then try again.'],
  },
  provider_billing: {
    headline: 'The provider’s account cannot pay for the repair',
    steps: ['Add credit to the provider’s account, or choose another provider in Eli settings.'],
  },
  rate_limited: {
    headline: 'The model provider is rate limiting you',
    steps: ['Wait a few minutes, then try again.', 'Check the quota on your provider account.'],
  },
  provider_credentials: {
    headline: 'The provider refused your key',
    steps: ['Update the key in Eli settings, then try again.'],
  },
  output_limit: {
    headline: 'The model ran out of room to answer',
    steps: ['Lower its reasoning effort, or choose a model that can write more, in Eli settings.'],
  },
  setup_needed: {
    headline: 'Eli’s access changed',
    steps: ['Check repair settings, then ask again about this failure.'],
  },
  checks_failed: {
    headline: 'Eli pushed a commit and the checks still fail',
    steps: [
      'Open the commit to see what changed and which checks failed.',
      'Another attempt starts from the new head, so it sees Eli’s change.',
    ],
  },
  interrupted: {
    headline: 'The controller restarted while Eli was working',
    steps: ['Check whether a commit was pushed to the branch, then try again.'],
  },
  unverified: {
    headline: 'No complete check result was seen',
    steps: ['Open the pull request’s checks before you accept the fix.'],
  },
  superseded: {
    headline: 'The pull request moved on',
    steps: [
      'Check the current pull request: a newer commit replaced the one Eli worked on, or its checks already passed.',
    ],
  },
  blocked: {
    headline: 'Something stopped the repair before it began',
    steps: ['Read the message above for what to fix, then check readiness again.'],
  },
  succeeded: {
    headline: 'The checks on Eli’s commit passed',
    steps: ['Review the commit as you would any other change. Eli never merges.'],
  },
  queued: STILL_WORKING,
  working: STILL_WORKING,
  checking: STILL_WORKING,
};

/**
 * The advice for a repair. A failure code outranks the state the repair ended
 * in, because the code says why; a code this build does not know, and a failure
 * with none, get the advice that is true of any repair that did not produce a
 * fix, so the card is never empty.
 */
export function guidanceFor(repair: Pick<Schemas['EliRepair'], 'state' | 'details'>): Guidance {
  const code = repair.details?.failure_code ?? '';
  if (Object.hasOwn(TABLE, code)) return TABLE[code]!;
  if (Object.hasOwn(TABLE, repair.state)) return TABLE[repair.state]!;
  return UNSUPPORTED_FIX;
}
