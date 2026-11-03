import type { Job, Schemas } from '../api/types';
import { caseFromLive, type CaseView } from './case';
import { pullUrl } from './links';

/*
  What the hub works out from what it was given, so a test can hold it without a
  browser: the case behind each repair offer, and what the setup bar says.
*/

type Offer = Schemas['EliOpportunity'];

/**
 * How many running repairs the hub lists. A person has a handful at most, and a
 * list this long is read as a list rather than paged; the badge says how many
 * there are in all. It is not a page size the history offers, which is what lets
 * the history's own tests tell the two reads apart.
 */
export const RUNNING_LIMIT = 20;

/**
 * The case behind each offer, by the offer's id, built from the failed jobs of
 * its run.
 *
 * The hub asks for the jobs of every offer's run in one read, and a run id is a
 * key only within its repository, so what comes back may hold another
 * repository's run of the same number. Each offer takes only the jobs that are
 * its own, with the repository compared the way GitHub does, without regard to
 * case. An offer with no run, or whose jobs Zoomies has pruned, has a case with
 * nothing in it rather than none, so the card can still say what it is about.
 */
export function offerCases(
  offers: readonly Offer[],
  jobs: readonly Job[],
): Record<string, CaseView> {
  const out: Record<string, CaseView> = {};
  for (const offer of offers) {
    const repo = offer.repo.toLowerCase();
    const mine =
      offer.run_id > 0
        ? jobs.filter(
            (job) => job.github_run_id === offer.run_id && job.repo?.toLowerCase() === repo,
          )
        : [];
    const view = caseFromLive({
      repo: offer.repo,
      jobs: mine,
      readiness: null,
      run: offer.run_id > 0 ? offer.run_id : undefined,
    });
    // The offer knows its pull request, which no job does.
    view.pullNumber = offer.pull_number;
    if (offer.title) view.title = offer.title;
    const link = pullUrl(view.repoUrl, offer.pull_number);
    if (link) view.pullUrl = link;
    out[offer.id] = view;
  }
  return out;
}

export interface SetupSummary {
  state: 'loading' | 'ready' | 'needed' | 'off';
  /** The one word an operator looks for. */
  headline: string;
  /** What is known about the GitHub account, or what is missing. */
  account: string;
  /** How many repositories are on Ask me first, for an administrator and nobody else. */
  askFirst: number | null;
  /** How many repair themselves, counted the same way and for the same people. */
  automatic: number | null;
}

/**
 * Whether the person looking at the hub can repair, and what stands in the way.
 * It is about that person: another person's link does not make them ready. With
 * sign-in off there is nobody to repair as, which outranks everything else, and
 * until the settings arrive the bar says so rather than claiming either.
 */
export function setupSummary(input: {
  settings: Schemas['EliRepairSettings'] | null;
  userId: string | undefined;
  admin: boolean;
  authDisabled: boolean;
}): SetupSummary {
  const { settings, userId, admin, authDisabled } = input;
  if (authDisabled) {
    return {
      state: 'off',
      headline: 'Sign-in is off',
      account: 'Repairs need a signed-in person with a linked GitHub account',
      askFirst: null,
      automatic: null,
    };
  }
  if (!settings)
    return {
      state: 'loading',
      headline: 'Checking setup',
      account: '',
      askFirst: null,
      automatic: null,
    };

  const askFirst = admin
    ? settings.policies.filter((policy) => policy.enabled && policy.ask_first).length
    : null;
  const automatic = admin
    ? settings.policies.filter((policy) => policy.enabled && policy.automatic).length
    : null;
  const own = userId
    ? settings.identities.find((identity) => identity.user_id === userId)
    : undefined;
  if (admin) {
    const configured = settings.policies.some((policy) => policy.enabled);
    return {
      state: configured ? 'ready' : 'needed',
      headline: configured ? 'Repository repairs configured' : 'Choose repositories',
      account: configured
        ? 'Readiness is checked for each repair'
        : 'Select repositories and choose when Eli should fix failed checks',
      askFirst,
      automatic,
    };
  }
  if (!own) {
    return {
      state: 'needed',
      headline: 'Setup needed',
      account: 'No GitHub account linked',
      askFirst,
      automatic,
    };
  }
  if (!own.confirmed) {
    return {
      state: 'needed',
      headline: 'Setup needed',
      account: `GitHub @${own.github_login} linked, repairs not allowed yet`,
      askFirst,
      automatic,
    };
  }
  return {
    state: 'ready',
    headline: 'Personal GitHub account linked',
    account: `GitHub @${own.github_login} linked; provider and PR readiness are checked when you request a repair`,
    askFirst,
    automatic,
  };
}
