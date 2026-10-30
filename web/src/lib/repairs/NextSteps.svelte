<!--
  What to do about how a repair ended. The advice is the guidance table's, keyed
  by the failure code the worker recorded or else by the state, so the card is
  never empty and never invents a cause. "Try another attempt" opens the repair
  page for the same pull request, which checks readiness again before it offers
  anything: the failure may have been fixed, or moved on, since.
-->
<script lang="ts">
  import { ExternalLink } from '@lucide/svelte';
  import type { Schemas } from '$lib/api/types';
  import Button from '$lib/components/Button.svelte';
  import Panel from '$lib/components/Panel.svelte';
  import type { CaseView } from './case';
  import { guidanceFor } from './guidance';
  import { safeHttpUrl } from './links';
  import { targetHref } from './workspace';

  interface Props {
    repair: Schemas['EliRepair'];
    view: CaseView;
  }

  let { repair, view }: Props = $props();

  const guidance = $derived(guidanceFor(repair));
  const succeeded = $derived(repair.state === 'succeeded');
  const again = $derived(
    targetHref(
      repair.pull_number > 0
        ? { repo: repair.repo, pull: repair.pull_number }
        : { repo: repair.repo, run: repair.run_id },
    ),
  );
  const pullLink = $derived(safeHttpUrl(view.pullUrl));
</script>

<Panel title={succeeded ? 'What to do next' : 'What to try next'} description={guidance.headline}>
  <ol class="steps">
    {#each guidance.steps as step (step)}
      <li>{step}</li>
    {/each}
  </ol>
  <div class="go">
    {#if !succeeded}<Button href={again}>Try another attempt</Button>{/if}
    {#if pullLink}
      <Button variant="secondary" href={pullLink} newTab iconAfter={ExternalLink}
        >Open pull request</Button
      >
    {/if}
    <Button variant="ghost" href="/repairs/setup">Repair settings</Button>
  </div>
</Panel>

<style>
  .steps {
    display: grid;
    gap: var(--z-space-3);
    margin: 0;
    padding: 0;
    list-style: none;
    counter-reset: step;
  }
  .steps li {
    display: grid;
    grid-template-columns: 1.5rem minmax(0, 1fr);
    gap: var(--z-space-3);
    overflow-wrap: anywhere;
  }
  .steps li::before {
    counter-increment: step;
    content: counter(step);
    display: grid;
    place-items: center;
    width: 1.375rem;
    height: 1.375rem;
    border: var(--z-border-width) solid var(--z-border-strong);
    border-radius: var(--z-radius-full);
    color: var(--z-text-muted);
    font-size: var(--z-text-xs);
    font-weight: var(--z-weight-semibold);
  }
  .go {
    display: flex;
    flex-wrap: wrap;
    gap: var(--z-space-2);
    margin-top: var(--z-space-5);
  }
</style>
