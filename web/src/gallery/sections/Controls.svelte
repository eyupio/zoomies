<!--
  Buttons, icon buttons, segmented controls and tooltips, in every variant and
  in the states that are easy to forget: loading, refused-with-a-reason and
  pressed. A refusal has to stay reachable, so these are `aria-disabled` and
  keep their reason in a tooltip and in the accessible description.
-->
<script lang="ts">
  import { Plus, RefreshCw, Star, Trash2, Power } from '@lucide/svelte';
  import Button from '$lib/components/Button.svelte';
  import IconButton from '$lib/components/IconButton.svelte';
  import Panel from '$lib/components/Panel.svelte';
  import Segmented from '$lib/components/Segmented.svelte';
  import FilterChips from '$lib/components/FilterChips.svelte';
  import { runnerStateChips } from '$lib/runners/state-chips';
  import type { RunnerState } from '$lib/api/types';
  import { THEME_OPTIONS } from '$lib/state/theme.svelte';

  let pressed = $state(false);
  let range = $state('15m');
  let chipStates = $state<RunnerState[]>(['busy', 'draining']);
  const chipOptions = runnerStateChips(
    { provisioning: 1, registering: 1, idle: 3, busy: 3, draining: 1, failed: 1 },
    false,
    true,
  );

  const ranges = [
    { value: '5m', label: '5m', name: 'Last five minutes' },
    { value: '15m', label: '15m', name: 'Last fifteen minutes' },
    { value: '1h', label: '1h', name: 'Last hour' },
    { value: '24h', label: '24h', name: 'Last twenty-four hours' },
  ];
</script>

<Panel title="Controls" description="Variants, sizes and the states a click is refused in.">
  <div class="block">
    <h3>Button variants</h3>
    <div class="row">
      <Button variant="primary">Create a pool</Button>
      <Button>Import</Button>
      <Button variant="ghost">Cancel</Button>
      <Button variant="danger">Delete pool</Button>
    </div>
  </div>

  <div class="block">
    <h3>Button sizes</h3>
    <div class="row baseline">
      <Button size="sm">Small</Button>
      <Button size="md">Medium</Button>
      <Button size="lg">Large (sign-in and the first-account form)</Button>
    </div>
  </div>

  <div class="block">
    <h3>Working, and what the keyboard can do</h3>
    <div class="row">
      <Button variant="primary" loading>Saving</Button>
      <Button icon={RefreshCw} iconSpin help="Fetch this page again." keys={['r']}>Refresh</Button>
      <Button help="A new pool starts paused, so nothing is sent to it until you resume it.">
        Create a pool
      </Button>
      <Button href="https://zoomies.sh/" newTab>Release notes</Button>
    </div>
  </div>

  <div class="block">
    <h3>Off, and refused with a reason</h3>
    <p class="note">
      The first is plainly disabled and cannot be focused. The others keep focus and pointer, so the
      sentence saying why is reachable by keyboard as well as by mouse.
    </p>
    <div class="row">
      <Button disabled>Publish</Button>
      <Button
        variant="danger"
        disabled
        lock
        reason="Only administrators can delete a pool. Ask an administrator for the administrator role."
      >
        Delete pool
      </Button>
      <Button disabled reason="The pool is paused. Resume it to send work here.">Run now</Button>
    </div>
  </div>

  <div class="block">
    <h3>Icon buttons</h3>
    <div class="row baseline">
      <IconButton icon={Plus} label="Create a pool" />
      <IconButton icon={Plus} label="Create a pool" variant="secondary" />
      <IconButton
        icon={Power}
        label="Toggle this pool"
        variant="secondary"
        {pressed}
        onclick={() => (pressed = !pressed)}
      />
      <IconButton
        icon={Star}
        label="Favourite this pool"
        help="Pinned pools sort first on the Overview."
      />
      <IconButton
        icon={Trash2}
        label="Delete this pool"
        variant="danger"
        disabled
        lock
        reason="Only administrators can delete a pool. Ask an administrator for the administrator role."
      />
    </div>
  </div>

  <div class="block">
    <h3>Filter chips</h3>
    <p class="note">
      A list's primary filter: All and one chip per value with its count, more than one in force at
      once, each saying so with a tick as well as aria-pressed.
    </p>
    <FilterChips
      label="Filter by runner state"
      options={chipOptions}
      selected={chipStates}
      total={11}
      onchange={(next) => (chipStates = next)}
    />
  </div>

  <div class="block">
    <h3>Segmented</h3>
    <p class="note">
      One choice among a few, with the choice in force filled rather than bordered.
    </p>
    <div class="row">
      <Segmented
        label="Activity range"
        options={ranges}
        value={range}
        onchange={(v) => (range = v)}
      />
      <Segmented
        label="Colour mode, disabled"
        options={THEME_OPTIONS}
        value="system"
        disabled
        onchange={() => {}}
      />
    </div>
  </div>
</Panel>

<style>
  .block + .block {
    margin-top: var(--z-space-5);
  }
  h3 {
    margin: 0 0 var(--z-space-2);
    font-size: var(--z-text-xs);
    font-weight: var(--z-weight-semibold);
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: var(--z-text-muted);
  }
  .row {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--z-space-3);
  }
  .row.baseline {
    align-items: baseline;
  }
  .note {
    max-width: 80ch;
    margin: 0 0 var(--z-space-3);
    font-size: var(--z-text-xs);
    color: var(--z-text-muted);
  }
</style>
