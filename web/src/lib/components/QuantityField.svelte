<!--
  A size, as a slider and a field that reads what a person writes.

  The slider is for choosing: its notches are values somebody has a reason to
  pick, and the words under them say which. The field is for knowing: an
  operator who already has "4g" from a compose file, or "1.5" cores from a
  ticket, types it, and the field writes back what it understood in the
  largest unit that says it exactly -- 4096mb becomes "4 GB". The two are one
  value, so moving either moves the other.

  The field commits on Enter and when it loses focus, not on every keystroke:
  a limit that became 4 MB, then 40, then 409 on the way to 4096 would have
  the page argue with every intermediate figure. What cannot be read is said
  beside the field and changes nothing.

  Without `values` it is the field alone, for a figure with no sensible
  notches.
-->
<script lang="ts">
  import type { ComponentProps } from 'svelte';
  import Input from './Input.svelte';
  import Slider from './Slider.svelte';
  import { formatQuantity, parseQuantity, type Quantity } from '$lib/units';

  interface Props {
    /** In the quantity's own unit: cores, megabytes or gigabytes. */
    value: number | null;
    quantity: Quantity;
    /** The accessible name of the slider; the field takes the Field's label. */
    label: string;
    /** The slider's notches. Leave out for a field with no slider. */
    values?: readonly number[];
    marks?: ComponentProps<typeof Slider>['marks'];
    tone?: 'accent' | 'warning';
    /** The words for a notch. Defaults to the field's own spelling. */
    valuetext?: (value: number) => string;
    /**
     * What an empty field means, where emptiness is an answer -- "no limit",
     * "the template decides". Left out, clearing the field puts it back.
     */
    empty?: { value: number | null; placeholder: string };
    /** Refuse a fraction, for a figure the API keeps whole -- cores held back on a host. */
    whole?: boolean;
    id?: string;
    /** The field's accessible name where no Field label is pointed at it. */
    fieldLabel?: string;
    describedBy?: string;
    invalid?: boolean;
    disabled?: boolean;
    onchange?: (value: number | null) => void;
  }

  let {
    value = $bindable(),
    quantity,
    label,
    values,
    marks,
    tone = 'accent',
    valuetext,
    empty,
    whole = false,
    id,
    fieldLabel,
    describedBy,
    invalid = false,
    disabled = false,
    onchange,
  }: Props = $props();

  const spell = (v: number | null): string => {
    if (v === null || (empty && v === empty.value)) return '';
    return formatQuantity(v, quantity);
  };

  let text = $state('');
  let error = $state('');
  let editing = $state(false);

  /* The field follows the value while nobody is typing in it, so a slider
     move, a "use the default" button or a draft loaded late all show up. */
  $effect(() => {
    const shown = spell(value);
    if (!editing) text = shown;
  });

  const uid = $props.id();
  const errorId = $derived(`${id ?? uid}-unreadable`);
  const described = $derived(
    [describedBy, error ? errorId : undefined].filter(Boolean).join(' ') || undefined,
  );

  function set(next: number | null): void {
    error = '';
    if (next === value) {
      text = spell(next);
      return;
    }
    value = next;
    text = spell(next);
    onchange?.(next);
  }

  function commit(): void {
    editing = false;
    const parsed = parseQuantity(text, quantity);
    if (!parsed.ok) {
      error = parsed.error;
      return;
    }
    if (whole && parsed.value !== null && !Number.isInteger(parsed.value)) {
      error = 'Write a whole number here, such as 2.';
      return;
    }
    if (parsed.value === null) {
      if (empty) set(empty.value);
      else text = spell(value);
      error = '';
      return;
    }
    set(parsed.value);
  }

  function key(event: KeyboardEvent): void {
    if (event.key === 'Enter') {
      // Enter inside a dialog's form would otherwise submit it with the
      // figure still unread.
      event.preventDefault();
      commit();
    } else if (event.key === 'Escape' && editing) {
      event.stopPropagation();
      editing = false;
      error = '';
      text = spell(value);
    }
  }
</script>

<div class="quantity" class:alone={!values}>
  {#if values}
    <Slider
      {values}
      value={value ?? 0}
      {label}
      valuetext={valuetext ?? ((v) => formatQuantity(v, quantity))}
      {marks}
      {tone}
      {disabled}
      readout={false}
      describedBy={described}
      onchange={(v) => set(empty && v === 0 ? empty.value : v)}
    />
  {/if}
  <div class="entry">
    <Input
      bind:value={text}
      {id}
      ariaLabel={fieldLabel}
      describedBy={described}
      invalid={invalid || error !== ''}
      {disabled}
      placeholder={empty?.placeholder}
      size="sm"
      mono
      spellcheck={false}
      autocomplete="off"
      oninput={() => (editing = true)}
      onblur={commit}
      onkeydown={key}
    />
    {#if error}
      <p class="unreadable" id={errorId} role="alert">{error}</p>
    {/if}
  </div>
</div>

<style>
  .quantity {
    display: grid;
    grid-template-columns: 1fr 8rem;
    align-items: start;
    gap: var(--z-space-4);
  }
  .quantity.alone {
    grid-template-columns: minmax(0, 12rem);
  }
  .entry {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-1);
  }
  .unreadable {
    margin: 0;
    color: var(--z-danger);
    font-size: var(--z-text-xs);
  }
  @media (max-width: 640px) {
    .quantity {
      grid-template-columns: 1fr;
    }
  }
</style>
