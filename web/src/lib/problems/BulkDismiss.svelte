<!--
  Put away everything listed at once: a menu of how long, as each problem's own
  menu is, with "until resolved" last as the most drastic. The drawer's header
  and the Overview's Needs attention card carry the same control, so a decision
  made at either means the same thing and is listed as dismissed in the drawer.
-->
<script lang="ts">
  import { CheckCheck, Clock, X } from '@lucide/svelte';
  import DropdownMenu, { type MenuItem } from '$lib/components/DropdownMenu.svelte';
  import { notifications, SNOOZE_OPTIONS } from '$lib/state/notifications.svelte';

  interface Props {
    size?: 'sm' | 'md';
  }
  let { size = 'sm' }: Props = $props();

  const items = $derived<MenuItem[]>([
    ...SNOOZE_OPTIONS.map((option) => ({
      id: `snooze-all-${option.id}`,
      label: `Snooze all for ${option.label}`,
      icon: Clock,
      onSelect: () => notifications.snoozeAll(option.ms),
    })),
    {
      id: 'dismiss-all',
      label: 'Dismiss all until resolved',
      icon: X,
      separated: true,
      onSelect: () => notifications.dismissAll(),
    },
  ]);
</script>

<DropdownMenu
  label="Dismiss all"
  triggerLabel="Dismiss all"
  triggerIcon={CheckCheck}
  {items}
  {size}
/>
