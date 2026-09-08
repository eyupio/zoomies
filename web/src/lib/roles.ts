/**
 * The three roles, in one place, in the words an operator sees.
 *
 * There were two copies of this list -- one in the accounts panel, one in the
 * tokens panel -- and they had already drifted: an administrator "also manages
 * accounts, tokens, installations and settings" in the first and "everything,
 * including accounts and settings" in the second. A third place printed the
 * bare id, so an operator was told their role was `admin` on one screen and
 * Administrator on the next.
 *
 * `internal/auth/rbac.go` is where the roles are actually enforced; this is
 * only how they are named and described.
 */
import type { Role } from './api/types';

export interface RoleOption {
  value: Role;
  label: string;
  /** One sentence, in ascending order of what it can do. */
  description: string;
}

export const ROLE_OPTIONS: readonly RoleOption[] = [
  { value: 'viewer', label: 'Viewer', description: 'Reads everything except secrets.' },
  { value: 'operator', label: 'Operator', description: 'Acts on the fleet and manages pools.' },
  {
    value: 'admin',
    label: 'Administrator',
    description: 'Also manages accounts, tokens, installations and settings.',
  },
];

/**
 * The display name for a role. Unknown values are shown as they arrived rather
 * than hidden: a role this build does not know about is worth seeing.
 */
export function roleLabel(role: string | undefined | null): string {
  if (!role) return 'Unknown';
  return ROLE_OPTIONS.find((option) => option.value === role)?.label ?? role;
}
