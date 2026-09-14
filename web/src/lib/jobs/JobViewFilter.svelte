<!--
  The Jobs page's status filter, as a row of buttons rather than a menu.

  Status is the filter this page exists to apply, so it is the one thing that
  is never hidden behind a control that has to be opened first. Each button
  wears its own status shape and colour from the state map, so the row reads as
  the lifecycle it is -- running, queued, failed, finished -- rather than as
  four words.

  Buttons with `aria-pressed`, as Segmented does, rather than radios: the
  choice takes effect at once, there is nothing to submit, and a screen reader
  says "pressed" for the one in force, which is the whole state. A view nobody
  has a button for -- a conclusion facet, the unmatched switch -- leaves every
  button unpressed, because saying one of them is in force would be a lie the
  chips above the grid immediately contradict.
-->
<script lang="ts">
  import StatusDot from '$lib/components/StatusDot.svelte';
  import { JOB_VIEWS, type JobView } from './views';

  interface Props {
    /** The view in force, or `''` when the filters are something else. */
    value: JobView | '';
    onchange: (view: JobView) => void;
  }

  let { value, onchange }: Props = $props();
</script>

<div class="views" role="group" aria-label="Filter jobs by status">
  {#each JOB_VIEWS as view (view.id)}
    <button
      type="button"
      data-view={view.id}
      aria-pressed={value === view.id}
      title={view.hint}
      onclick={() => onchange(view.id)}
    >
      <!--
        The shape is hidden from assistive technology: it says the same word
        the button already says, and a button called "Running Running" is a
        button nobody can ask for by name.
      -->
      {#if view.status}<span class="shape" aria-hidden="true"
          ><StatusDot status={view.status} size="sm" /></span
        >{/if}
      {view.label}
    </button>
  {/each}
</div>

<style>
  .views {
    display: flex;
    flex-wrap: wrap;
    gap: var(--z-space-2);
  }
  button {
    display: inline-flex;
    align-items: center;
    gap: var(--z-space-2);
    height: var(--z-space-8);
    padding: 0 var(--z-space-3);
    border: var(--z-border-width) solid var(--z-border-strong);
    border-radius: var(--z-radius-md);
    background: var(--z-surface);
    color: var(--z-text-muted);
    font: inherit;
    font-size: var(--z-text-sm);
    font-weight: var(--z-weight-medium);
    white-space: nowrap;
    cursor: pointer;
    transition:
      background-color var(--z-motion-fast) var(--z-ease),
      border-color var(--z-motion-fast) var(--z-ease),
      color var(--z-motion-fast) var(--z-ease);
  }
  button:hover {
    background: var(--z-surface-hover);
    color: var(--z-text);
  }
  button[aria-pressed='true'] {
    border-color: var(--z-accent-border);
    background: var(--z-accent-subtle);
    color: var(--z-accent);
  }
  button:focus-visible {
    outline: var(--z-focus-width) solid var(--z-focus-colour);
    outline-offset: var(--z-focus-offset);
  }
  /*
    The shape carries the status, so it keeps its own colour whichever button
    is pressed -- an operator learns that a filled circle is a job running, and
    a row where the pressed button recoloured its dot would teach them
    otherwise.
  */
  .shape {
    display: inline-flex;
    flex: none;
  }
</style>
