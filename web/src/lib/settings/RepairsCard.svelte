<!--
  What Settings says about PR repairs now that they have pages of their own: one
  sentence on what they are, and the two places to go. The model they use is chosen
  above, on this page; everything about the repairs themselves is on the hub and
  on the setup page.
-->
<script lang="ts">
  import { GitPullRequest } from '@lucide/svelte';
  import Button from '$lib/components/Button.svelte';

  const headingId = $props.id();
</script>

<section class="card" aria-labelledby={headingId}>
  <GitPullRequest size={20} aria-hidden="true" />
  <div class="text">
    <h3 id={headingId}>PR repairs</h3>
    <p>
      Eli diagnoses a failure, pushes one repair commit and watches CI. Normal PR review stays in
      place. Offers, running repairs and history are on the Eli repairs page, and linking your
      GitHub account and choosing repositories is on the setup page.
    </p>
    <div class="go">
      <Button href="/repairs">Open Eli repairs</Button>
      <Button variant="secondary" href="/repairs/setup">Repair settings</Button>
    </div>
  </div>
</section>

<style>
  .card {
    display: flex;
    gap: var(--z-space-3);
    margin-top: var(--z-space-6);
    padding: var(--z-space-4);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-lg);
    background: var(--z-surface);
  }
  .card > :global(svg) {
    flex: none;
    margin-top: var(--z-nudge-2);
  }
  .text {
    min-width: 0;
  }
  h3 {
    margin: 0;
    font-size: var(--z-text-base);
    font-weight: var(--z-weight-semibold);
  }
  p {
    margin: var(--z-space-2) 0 var(--z-space-4);
    color: var(--z-text-muted);
    font-size: var(--z-text-sm);
    line-height: var(--z-leading-lg);
  }
  .go {
    display: flex;
    flex-wrap: wrap;
    gap: var(--z-space-2);
  }
</style>
