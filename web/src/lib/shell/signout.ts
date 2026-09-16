/**
 * Signing out, from wherever the control is.
 *
 * The account menu on a desktop and the sheet a phone's More button opens both
 * offer it, and the order matters: the event stream is stopped before the
 * session is ended, or the stream reconnects into a 401 and the shell shows the
 * sign-in form for a moment before the navigation reaches it.
 */
import { router } from '../router';
import { fleet } from '../state/fleet.svelte';
import { session } from '../state/session.svelte';
import { toasts } from '../state/toasts.svelte';

export async function signOut(): Promise<void> {
  try {
    fleet.stop();
    await session.logout();
    router.navigate('/login');
  } catch (cause) {
    toasts.fromError(cause, 'Could not sign out');
  }
}
