<!--
  Settings on a phone: the list of pages, since there is no room for a rail
  beside one. Each row says what the page holds, and a page the signed-in role
  cannot open says so in the same place rather than being left out.
-->
<script lang="ts">
  import { ChevronRight, Lock } from '@lucide/svelte';
  import { session } from '$lib/state/session.svelte';
  import PageHeader from '$lib/components/PageHeader.svelte';
  import { SETTINGS_GROUPS, settingsPath } from './pages';

  const uid = $props.id();
</script>

<PageHeader
  title="Settings"
  subtitle="Your account, who else can sign in, and what this controller runs with."
/>

<div class="groups">
  {#each SETTINGS_GROUPS as group, i (group.label)}
    <section aria-labelledby="settings-index-{uid}-{i}">
      <h2 id="settings-index-{uid}-{i}">{group.label}</h2>
      <ul>
        {#each group.pages as page (page.id)}
          {@const locked = !session.can(page.needs)}
          <li>
            <a href={settingsPath(page.id)} class:locked>
              <page.icon size={18} aria-hidden="true" />
              <span class="text">
                <span class="label">{page.label}</span>
                <span class="description">
                  {locked ? 'Needs the administrator role.' : page.description}
                </span>
              </span>
              {#if locked}
                <Lock size={14} aria-hidden="true" />
              {:else}
                <ChevronRight size={16} aria-hidden="true" />
              {/if}
            </a>
          </li>
        {/each}
      </ul>
    </section>
  {/each}
</div>

<style>
  .groups {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-5);
  }
  h2 {
    margin: 0 0 var(--z-space-2);
    padding: 0 var(--z-space-1);
    font-size: var(--z-text-2xs);
    line-height: var(--z-leading-2xs);
    font-weight: var(--z-weight-medium);
    letter-spacing: var(--z-tracking-wide);
    text-transform: uppercase;
    color: var(--z-text-subtle);
  }
  ul {
    margin: 0;
    padding: 0;
    list-style: none;
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-md);
    background: var(--z-surface);
    overflow: hidden;
  }
  li + li {
    border-top: var(--z-border-width) solid var(--z-border);
  }
  a {
    display: flex;
    align-items: center;
    gap: var(--z-space-3);
    min-height: var(--z-space-12);
    padding: var(--z-space-2) var(--z-space-3) var(--z-space-2) var(--z-space-4);
    color: var(--z-text);
    text-decoration: none;
  }
  a:hover {
    background: var(--z-surface-hover);
  }
  a > :global(svg) {
    flex: none;
    color: var(--z-text-muted);
  }
  a.locked {
    color: var(--z-text-muted);
  }
  .text {
    flex: 1;
    min-width: 0;
  }
  .label {
    display: block;
    font-size: var(--z-text-base);
    line-height: var(--z-leading-base);
    font-weight: var(--z-weight-medium);
  }
  .description {
    display: block;
    font-size: var(--z-text-xs);
    line-height: var(--z-leading-xs);
    color: var(--z-text-subtle);
  }
</style>
