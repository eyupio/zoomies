<!--
  Always-visible entry point for repairing a pull request from its GitHub URL.
  Checking never starts a repair or spends an attempt: it moves to the review
  page for that pull request.
-->
<script lang="ts">
  import Button from '$lib/components/Button.svelte';
  import Field from '$lib/components/Field.svelte';
  import Input from '$lib/components/Input.svelte';
  import { router } from '$lib/router';
  import { parseRepairTarget } from './target';
  import { targetHref } from './workspace';

  interface Props {
    /** Omit the outer card chrome when placed inside another card. */
    bare?: boolean;
  }

  let { bare = false }: Props = $props();

  let value = $state('');
  let error = $state('');

  function submit(event: SubmitEvent): void {
    event.preventDefault();
    const target = parseRepairTarget(value);
    if (!target) {
      error = 'Paste a GitHub pull request URL, such as https://github.com/owner/repo/pull/123.';
      return;
    }
    error = '';
    value = '';
    router.navigate(targetHref({ repo: target.repo, pull: target.pull_number }));
  }
</script>

<form class="bar" class:bare onsubmit={submit}>
  <Field
    label="Pull request"
    hint="Paste a GitHub pull request URL, such as https://github.com/owner/repo/pull/123. Eli checks it before anything is spent."
    {error}
  >
    {#snippet children({ id, describedBy, invalid })}
      <div class="row">
        <div class="input">
          <Input
            {id}
            {describedBy}
            {invalid}
            bind:value
            placeholder="https://github.com/owner/repo/pull/123"
            autocomplete="off"
            spellcheck={false}
          />
        </div>
        <Button variant="primary" type="submit">Check this pull request</Button>
      </div>
    {/snippet}
  </Field>
</form>

<style>
  .bar {
    padding: var(--z-space-3) var(--z-space-4);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-md);
    background: var(--z-surface);
  }
  .bar.bare {
    width: 100%;
    padding: 0;
    border: 0;
    border-radius: 0;
    background: none;
  }
  .row {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--z-space-2);
    min-width: 0;
  }
  .input {
    flex: 1 1 16rem;
    min-width: 0;
  }
</style>
