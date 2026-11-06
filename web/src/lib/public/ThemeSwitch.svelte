<!--
  Light, dark or the system's choice, as three icons in one pill.

  The pages before sign in have no top bar to keep this in, and a visitor on a
  phone at night is the person most likely to want it, so it sits in the public
  header where it can be found without a menu. It is the same choice the
  Appearance page makes: same store, same words, same three values.

  Buttons with aria-pressed rather than radios, as Segmented does: the choice
  takes effect at once and a screen reader says "pressed" for the one in force.

  The name is a native title and not the shared Tooltip: this control is on the
  status page too, which is held to a few kilobytes because it is read by people
  with no account on a phone, and the tooltip is most of that on its own.
-->
<script lang="ts">
  import { Monitor, Moon, Sun } from '@lucide/svelte';
  import type { LucideIcon } from '@lucide/svelte';
  import { THEME_OPTIONS, theme, type ThemeChoice } from '$lib/state/theme.svelte';

  /** Light first, then dark, then the system: the order people read a sun and a moon in. */
  const ORDER: readonly ThemeChoice[] = ['light', 'dark', 'system'];
  const ICONS: Record<ThemeChoice, LucideIcon> = { light: Sun, dark: Moon, system: Monitor };
  const options = ORDER.map((value) => THEME_OPTIONS.find((option) => option.value === value)!);

  interface Props {
    /**
     * Show a choice that is not the stored one, with `onchange` to hear about a
     * press. The component gallery uses this so reviewing the switch never
     * rewrites the developer's real Appearance setting; a page leaves both out.
     */
    value?: ThemeChoice;
    onchange?: (value: ThemeChoice) => void;
  }
  const { value, onchange }: Props = $props();
  const current = $derived(value ?? theme.choice);
  const choose = (next: ThemeChoice): void => (onchange ? onchange(next) : theme.set(next));
</script>

<div class="switch" role="group" aria-label="Appearance">
  {#each options as option (option.value)}
    {@const Icon = ICONS[option.value]}
    <button
      type="button"
      aria-pressed={current === option.value}
      aria-label={option.name}
      title={option.name}
      onclick={() => choose(option.value)}
    >
      <Icon size={16} aria-hidden="true" />
    </button>
  {/each}
</div>

<style>
  .switch {
    display: inline-flex;
    flex: none;
    gap: var(--z-space-1);
    padding: var(--z-space-1);
    border: var(--z-border-width) solid var(--z-border-strong);
    border-radius: var(--z-radius-md);
    background: var(--z-surface);
  }
  button {
    display: inline-grid;
    place-items: center;
    /* Small beside a pointer and the touch size under a finger. */
    width: var(--z-space-8);
    height: var(--z-space-8);
    padding: 0;
    border: var(--z-border-width) solid transparent;
    border-radius: var(--z-radius-sm);
    background: transparent;
    color: var(--z-text-muted);
    cursor: pointer;
    transition:
      background-color var(--z-motion-fast) var(--z-ease),
      color var(--z-motion-fast) var(--z-ease);
  }
  button:hover {
    color: var(--z-text);
  }
  /* The one in force is lifted off the pill, as the portal's is. */
  button[aria-pressed='true'] {
    border-color: var(--z-border-strong);
    background: var(--z-surface-raised);
    color: var(--z-text);
  }
  @media (pointer: coarse), (max-width: 800px) {
    button {
      width: var(--z-control-touch);
      height: var(--z-control-touch);
    }
  }
  button:focus-visible {
    outline: var(--z-focus-width) solid var(--z-focus-colour);
    outline-offset: var(--z-focus-offset);
  }
</style>
