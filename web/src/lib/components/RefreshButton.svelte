<!--
  Refresh: the same control, in the same place, on every page that has something
  to fetch.

  `PageHeader` renders it, first in the row of actions, so an operator learns one
  position rather than nine. It is a secondary button on purpose -- the primary
  action of a page is the thing that changes the fleet, not the thing that
  re-reads it -- and it is deliberately quiet: nothing here is stale, and the
  control must not suggest otherwise.

  What it does with a press:

  * the icon turns for as long as the fetch takes, and for a beat longer, so a
    refresh answered out of a warm cache still reads as having happened;
  * the label never moves, because a control that resizes under the pointer is
    a control that gets clicked twice;
  * the tooltip says when the last one landed, which is the question somebody
    reaching for this button is usually actually asking;
  * `aria-busy` and one polite announcement carry all of that for anybody who is
    not watching the icon.

  The handler is registered with `refresh` rather than kept here, so the `R`
  shortcut and this button can never end up meaning different things.
-->
<script lang="ts">
  import { RotateCw } from '@lucide/svelte';
  import { onClockTick, relativeTime } from '../format';
  import { refresh, type RefreshHandler } from '../state/refresh.svelte';
  import Button from './Button.svelte';

  interface Props {
    /** What refreshing this page means. Also becomes what `R` does. */
    onrefresh: RefreshHandler;
    class?: string;
  }

  let { onrefresh, class: className = '' }: Props = $props();

  $effect(() => refresh.register(onrefresh));

  let now = $state(Date.now());

  $effect(() => onClockTick((tick) => (now = tick)));

  const busy = $derived(refresh.busy);

  const CLOCK = new Intl.DateTimeFormat(undefined, { timeStyle: 'medium' });

  const hint = $derived.by(() => {
    if (busy) return 'Refreshing this page.';
    const at = refresh.at;
    if (at === null) return 'Fetch this page again. Shortcut: R';
    return `Refreshed ${relativeTime(at, now)}. Fetch it again. Shortcut: R`;
  });

  /*
    Announced once per refresh rather than continuously: the wall clock in the
    sentence is what makes a second refresh a new announcement, and it is the
    part somebody listening actually wants.
  */
  let announcement = $state('');

  $effect(() => {
    if (refresh.count === 0) return;
    const at = refresh.at;
    announcement = at === null ? 'This page was refreshed.' : `Refreshed at ${timeOfDay(at)}.`;
  });

  function timeOfDay(ms: number): string {
    return CLOCK.format(ms);
  }
</script>

<Button
  variant="secondary"
  icon={RotateCw}
  iconSpin={busy}
  title={hint}
  class={className}
  onclick={() => void refresh.run()}
>
  Refresh
</Button>
<output class="sr-only" aria-live="polite">{announcement}</output>
