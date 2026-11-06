import {
  ApiError,
  pauseEliWatch,
  pauseEliWatchRepository,
  resumeEliWatch,
  resumeEliWatchRepository,
  stopEliWatch,
} from '$lib/api/client';
import { toasts } from '$lib/state/toasts.svelte';
import { changeWords, type RepositoryWatch, type Watch } from './watches';

/*
  The four controls the watching pages offer, in one place so the hub and the
  settings page make the same promise and report the same answer.

  Each returns what went wrong, or nothing, and says the rest itself. A page
  keeps its dialog open on an error and closes it on success, which is why the
  failure is returned rather than only toasted: the person pressed a button in a
  dialog, and a toast sliding in behind it is not an answer.
*/

const reason = (cause: unknown, fallback: string): string =>
  cause instanceof ApiError ? cause.message : fallback;

/** The reason kept with a pause, so a later reader is not left guessing. */
const PAUSE_REASON = 'Watching was paused from the Eli repairs page.';
const STOP_REASON = 'Watching was stopped from the Eli repairs page.';

export async function pauseWatch(watch: Watch): Promise<string> {
  try {
    await pauseEliWatch(watch.id, { reason: PAUSE_REASON });
    const words = changeWords('pause', watch, undefined);
    toasts.success(words.title, words.message);
    return '';
  } catch (cause) {
    const error = reason(cause, 'Could not pause this pull request. Try again.');
    const words = changeWords('pause', watch, error);
    toasts.error(words.title, words.message);
    return error;
  }
}

export async function resumeWatch(watch: Watch): Promise<string> {
  try {
    await resumeEliWatch(watch.id);
    const words = changeWords('resume', watch, undefined);
    toasts.success(words.title, words.message);
    return '';
  } catch (cause) {
    // A resume goes through admission again, so its refusal names what is
    // missing now: a disabled provider, a closed pull request, a paused policy.
    const error = reason(cause, 'Could not resume this pull request. Try again.');
    const words = changeWords('resume', watch, error);
    toasts.error(words.title, words.message);
    return error;
  }
}

export async function stopWatch(watch: Watch): Promise<string> {
  try {
    await stopEliWatch(watch.id, { reason: STOP_REASON });
    const words = changeWords('stop', watch, undefined);
    toasts.success(words.title, words.message);
    return '';
  } catch (cause) {
    const error = reason(cause, 'Could not stop this pull request. Try again.');
    const words = changeWords('stop', watch, error);
    toasts.error(words.title, words.message);
    return error;
  }
}

export async function pauseRepository(decision: RepositoryWatch): Promise<string> {
  try {
    await pauseEliWatchRepository(decision.id);
    toasts.success(
      `Stopped admitting new pull requests in ${decision.repo}`,
      'Pull requests already being followed carry on.',
    );
    return '';
  } catch (cause) {
    const error = reason(cause, 'Could not pause this repository. Try again.');
    toasts.error(`Could not pause ${decision.repo}`, error);
    return error;
  }
}

export async function resumeRepository(decision: RepositoryWatch): Promise<string> {
  try {
    await resumeEliWatchRepository(decision.id);
    toasts.success(
      `Resumed ${decision.repo}`,
      'Its open pull requests were read again, and a paused one stays paused.',
    );
    return '';
  } catch (cause) {
    const error = reason(cause, 'Could not resume this repository. Try again.');
    toasts.error(`Could not resume ${decision.repo}`, error);
    return error;
  }
}

/**
 * Stop every watch one by one, because there is no fleet-wide route and adding
 * one would be a new permission to get right. Each answer is counted, so a
 * partial stop is reported as what it was: some stopped, some did not, with the
 * first refusal named.
 */
export async function stopAllWatches(watches: readonly Watch[]): Promise<string> {
  const watching = watches.filter((watch) => watch.enabled);
  let stopped = 0;
  let firstError = '';
  for (const watch of watching) {
    try {
      await stopEliWatch(watch.id, { reason: STOP_REASON });
      stopped += 1;
    } catch (cause) {
      if (!firstError)
        firstError = reason(cause, `Could not stop ${watch.repo} #${watch.pull_number}.`);
    }
  }
  if (!firstError) {
    toasts.success(
      `Stopped ${stopped} ${stopped === 1 ? 'watch' : 'watches'}`,
      'Each one keeps its history. Admit a pull request again to follow it afresh.',
    );
    return '';
  }
  toasts.error(stopped > 0 ? `Stopped ${stopped}, not all` : 'Could not stop watching', firstError);
  return firstError;
}
