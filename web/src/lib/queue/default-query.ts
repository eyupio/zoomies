/**
 * The Queue page's default view, as a query: every job still waiting for a
 * runner here, paused ones included, and none an operator removed.
 *
 * Shared rather than copied, because the command centre's Waiting list says
 * "and N more" and links to the Queue: the two must be the same set of jobs,
 * or the link opens a list that disagrees with the number that offered it.
 */
import { WAITING_PROVISIONING, type Query } from '../api/types';

export const QUEUE_DEFAULT_QUERY = {
  provisioning: [...WAITING_PROVISIONING],
} satisfies Query<'listProvisioning'>;
