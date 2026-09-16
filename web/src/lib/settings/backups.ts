/**
 * Rendering one backup.
 *
 * The rules are here rather than in the panel because the table's badges and
 * the restore dialog's sentences have to agree about what a backup is: which
 * source it came from, whether this host's key opens it, and whether a restore
 * of it would be refused before anything moved.
 */
import {
  Archive,
  ArchiveRestore,
  Clock,
  CloudDownload,
  KeyRound,
  Terminal,
  Upload,
  UserRound,
} from '@lucide/svelte';
import type { LucideIcon } from '@lucide/svelte';
import type { Backup } from '$lib/api/types';
import type { StatusTone } from '$lib/status';

/** What each source is called, and how it is drawn. */
export const SOURCES: Record<
  Backup['source'],
  { label: string; icon: LucideIcon; tone: StatusTone | 'accent'; hint: string }
> = {
  scheduled: {
    label: 'Scheduled',
    icon: Clock,
    tone: 'neutral',
    hint: 'Taken by the controller on its own schedule.',
  },
  manual: {
    label: 'Taken here',
    icon: UserRound,
    tone: 'accent',
    hint: 'Taken from this page.',
  },
  cli: {
    label: 'Command line',
    icon: Terminal,
    tone: 'neutral',
    hint: 'Taken with zoomies backup on the controller host.',
  },
  uploaded: {
    label: 'Uploaded',
    icon: Upload,
    tone: 'busy',
    hint: 'Brought here from a file. Retention never counts or removes it.',
  },
  fetched: {
    label: 'From offsite',
    icon: CloudDownload,
    tone: 'busy',
    hint: 'Pulled back out of a backup remote. Retention never counts or removes it.',
  },
  'pre-migration': {
    label: 'Before an upgrade',
    icon: ArchiveRestore,
    tone: 'pending',
    hint: 'The copy the controller took before applying migrations. It has no manifest, so nothing about it can be checked before restoring except the database itself.',
  },
};

export function sourceOf(backup: Pick<Backup, 'source'>) {
  return (
    SOURCES[backup.source] ?? { label: backup.source, icon: Archive, tone: 'neutral', hint: '' }
  );
}

/** The badge that says whether this host's key opens the backup. */
export function keyStatus(
  backup: Pick<Backup, 'key_matches' | 'key_fingerprint' | 'key_included'>,
): {
  label: string;
  tone: StatusTone;
  icon: LucideIcon;
  hint: string;
} {
  if (backup.key_included) {
    return {
      label: 'Key inside',
      tone: 'danger',
      icon: KeyRound,
      hint: 'The encryption key is in this backup, so it decrypts itself. Treat the file as a credential.',
    };
  }
  if (backup.key_matches === true) {
    return {
      label: 'This key',
      tone: 'idle',
      icon: KeyRound,
      hint: `Sealed with the key this controller holds (${backup.key_fingerprint ?? ''}).`,
    };
  }
  if (backup.key_matches === false) {
    return {
      label: 'Other key',
      tone: 'danger',
      icon: KeyRound,
      hint: `Sealed with ${backup.key_fingerprint ?? 'another key'}, which this controller does not hold. A restore is refused until the right key is in place.`,
    };
  }
  return {
    label: 'No key recorded',
    tone: 'neutral',
    icon: KeyRound,
    hint: 'The backup does not say which key sealed it, so that cannot be checked before restoring.',
  };
}

/** A short form of a migration name: `0035_runner_fault_kind.sql` → `0035`. */
export function schemaShort(latest: string | undefined): string {
  if (!latest) return '--';
  const m = /^(\d+)/.exec(latest);
  return m?.[1] ?? latest.replace(/\.sql$/, '');
}

/**
 * Hand the browser a file. The object URL is revoked after the click has
 * been dispatched, which is the earliest moment it is safe to.
 */
export function saveBlob(blob: Blob, filename: string): void {
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = filename;
  a.rel = 'noopener';
  document.body.appendChild(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}

/** The interval as an operator reads it: "every 24h". */
export function describeInterval(interval: string): string {
  return interval ? `every ${interval}` : 'off';
}
