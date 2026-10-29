<!--
  The hub's one line about setup: whether the person looking can repair, which
  GitHub account that rests on, and, for an administrator, how many repositories
  repair themselves and how many ask first. The sentence about what is missing names the next thing to do, and
  Open settings is where it is done.

  It is about whoever is looking. Somebody else's linked account does not make
  this person ready, and with sign-in off there is nobody to repair as.
-->
<script lang="ts">
  import { ChevronRight } from '@lucide/svelte';
  import type { Schemas } from '$lib/api/types';
  import StatusDot from '$lib/components/StatusDot.svelte';
  import { pluralise } from '$lib/format';
  import { repairSetupStatus } from '$lib/status';
  import { setupSummary } from './hub';

  interface Props {
    /** Absent until the settings have been read. */
    settings: Schemas['EliRepairSettings'] | null;
    admin: boolean;
    /** The signed-in person, whose link is the one that counts. */
    userId?: string;
    authDisabled?: boolean;
  }

  let { settings, admin, userId, authDisabled = false }: Props = $props();

  const summary = $derived(setupSummary({ settings, userId, admin, authDisabled }));
  const status = $derived(repairSetupStatus(summary.state));
</script>

<section class="bar" aria-label="Repair settings">
  <span class="item">
    <StatusDot {status} size="sm" tip={false} />
    <strong>{summary.headline}</strong>
  </span>
  {#if summary.account}<span class="item">{summary.account}</span>{/if}
  {#if summary.automatic}
    <span class="item">
      Automatic on <strong>{pluralise(summary.automatic, 'repository', 'repositories')}</strong>
    </span>
  {/if}
  {#if summary.askFirst !== null}
    <span class="item">
      Ask me first on <strong>{pluralise(summary.askFirst, 'repository', 'repositories')}</strong>
    </span>
  {/if}
  <a class="more" href="/repairs/setup"
    >Open settings<ChevronRight size={14} aria-hidden="true" /></a
  >
</section>

<style>
  .bar {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--z-space-2) var(--z-space-6);
    padding: var(--z-space-3) var(--z-space-5);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-md);
    background: var(--z-surface);
    font-size: var(--z-text-sm);
  }
  .item {
    display: inline-flex;
    align-items: center;
    gap: var(--z-space-2);
    min-width: 0;
    overflow-wrap: anywhere;
  }
  strong {
    font-weight: var(--z-weight-semibold);
  }
  .more {
    display: inline-flex;
    align-items: center;
    gap: var(--z-space-1);
    margin-left: auto;
    color: var(--z-accent);
    text-decoration: none;
  }
  .more:hover {
    text-decoration: underline;
  }
  @media (max-width: 640px) {
    .bar {
      gap: var(--z-space-2) var(--z-space-4);
      padding: var(--z-space-3) var(--z-space-4);
    }
    .more {
      margin-left: 0;
    }
  }
</style>
