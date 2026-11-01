<!--
  A command the operator runs somewhere else, shown as the window they will
  run it in: a bar with a title, the command in the mono face, and the copy
  control beneath it.

  It stands on the dark stage, the same black in both themes, as the band and
  the pages before sign in do: a command is the one thing on a page that is
  not the page's own words, and the window says so before the reader parses
  it. The tokens the rules inside read are remapped to the panel's, so the copy
  control and the fine print need no colour of their own, and the component
  names no colour at all.

  A long command (an install line carries a token and an address) wraps
  inside the window rather than scrolling or widening the page: on a phone it
  is read and copied, never scrolled sideways through.
-->
<script lang="ts">
  import type { Snippet } from 'svelte';
  import CopyButton from './CopyButton.svelte';

  interface Props {
    /** The window's title: whose shell this is, or what the command is for. */
    title: string;
    /** The command, or one line per entry. A string is split on its line breaks. */
    command: string | readonly string[];
    /** The copy control's accessible name. Without it no control is drawn. */
    copyLabel?: string;
    size?: 'sm' | 'md';
    class?: string;
    /** Fine print under the command: what the token in it does, when it ends. */
    children?: Snippet;
  }
  let { title, command, copyLabel, size = 'md', class: className = '', children }: Props = $props();

  const lines = $derived(typeof command === 'string' ? command.split('\n') : [...command]);
  const whole = $derived(lines.join('\n'));
</script>

<div class="terminal {size} {className}">
  <div class="terminal-bar">
    <span class="dots" aria-hidden="true"><i></i><i></i><i></i></span>
    <span class="title">{title}</span>
  </div>
  <div class="terminal-body">
    {#each lines as line, i (i)}
      <pre><code>{line}</code></pre>
    {/each}
    {#if copyLabel || children}
      <div class="foot">
        {#if copyLabel}<CopyButton value={whole} label={copyLabel} size="md" showLabel />{/if}
        {#if children}<div class="fine">{@render children()}</div>{/if}
      </div>
    {/if}
  </div>
</div>

<style>
  .terminal {
    --z-text: var(--z-panel-text);
    --z-text-muted: var(--z-panel-text-muted);
    --z-text-subtle: var(--z-panel-text-subtle);
    --z-border: var(--z-panel-border);
    --z-border-strong: var(--z-panel-border);
    --z-surface: var(--z-panel-bg);
    --z-surface-hover: var(--z-panel-raised);
    --z-surface-raised: var(--z-panel-raised);
    --z-surface-sunken: var(--z-panel-raised);
    --z-accent: var(--z-brand-runner-blue);
    --z-focus-colour: var(--z-brand-runner-blue);
    --z-focus-gap: var(--z-panel-bg);
    overflow: hidden;
    min-width: 0;
    border: var(--z-border-width) solid var(--z-panel-border);
    border-radius: var(--z-radius-lg);
    background: var(--z-panel-bg);
    color: var(--z-panel-text);
  }
  .terminal-bar {
    display: flex;
    align-items: center;
    gap: var(--z-space-3);
    padding: var(--z-space-2) var(--z-space-4);
    border-bottom: var(--z-border-width) solid var(--z-panel-border);
    background: var(--z-panel-raised);
  }
  .dots {
    display: inline-flex;
    gap: var(--z-nudge-3);
  }
  .dots i {
    width: var(--z-space-2);
    height: var(--z-space-2);
    border-radius: var(--z-radius-full);
    background: var(--z-panel-border);
  }
  .title {
    min-width: 0;
    overflow-wrap: anywhere;
    color: var(--z-panel-text-muted);
    font-size: var(--z-text-xs);
    line-height: var(--z-leading-xs);
  }
  .terminal-body {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-2);
    padding: var(--z-space-4);
  }
  .sm .terminal-body {
    padding: var(--z-space-3);
  }
  pre {
    margin: 0;
    font-family: var(--z-font-mono);
    font-size: var(--z-text-sm);
    line-height: var(--z-leading-sm);
    color: var(--z-panel-text);
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  .foot {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--z-space-2) var(--z-space-4);
    margin-top: var(--z-space-2);
  }
  .fine {
    min-width: 0;
    color: var(--z-panel-text-muted);
    font-size: var(--z-text-xs);
    line-height: var(--z-leading-xs);
  }
  .fine :global(p) {
    margin: 0;
  }
</style>
