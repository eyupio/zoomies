<!--
  The Settings section's own navigation: a rail at the left where there is
  room, a strip above the page on a tablet.

  It lists every page, grouped, with the one being looked at marked, and it
  stays where it is while the page beside it scrolls -- so the map is always in
  view, and a seventh page is one more row rather than one more tab squeezed
  into a line that was already full.

  A page the signed-in role cannot open is listed with a lock rather than left
  out, and the reason is said once under the list.
-->
<script lang="ts">
  import { Lock } from '@lucide/svelte';
  import { roleLabel } from '$lib/roles';
  import { session } from '$lib/state/session.svelte';
  import { SETTINGS_GROUPS, settingsPath } from './pages';

  interface Props {
    /** The id of the page on screen. */
    current: string;
  }

  let { current }: Props = $props();

  const canAdmin = $derived(session.can('admin'));
  const uid = $props.id();
</script>

<nav class="rail" aria-label="Settings">
  <p class="rail-title">Settings</p>
  {#each SETTINGS_GROUPS as group, i (group.label)}
    <div class="group">
      <p class="group-label" id="settings-group-{uid}-{i}">{group.label}</p>
      <ul aria-labelledby="settings-group-{uid}-{i}">
        {#each group.pages as page (page.id)}
          {@const locked = !session.can(page.needs)}
          {@const here = page.id === current}
          <li>
            <a
              href={settingsPath(page.id)}
              aria-current={here ? 'page' : undefined}
              class:current={here}
              class:locked
            >
              <page.icon size={16} aria-hidden="true" />
              <span class="label">{page.label}</span>
              {#if locked}
                <Lock size={12} aria-hidden="true" class="lock" />
                <span class="sr-only">Needs the {roleLabel(page.needs)} role</span>
              {/if}
            </a>
          </li>
        {/each}
      </ul>
    </div>
  {/each}

  {#if !canAdmin}
    <p class="note">
      Some of these pages need a role above yours, users, MCP clients and the configuration need the
      administrator role, and backups the platform one. You are signed in with the {roleLabel(
        session.role,
      )} role, so those pages are listed but not open to you.
    </p>
  {/if}

  <!--
    No block saying whose settings these are at the foot of the rail. The
    Account row two inches above is that answer, and the Account page says it in
    full; the block repeated it in a column narrow enough to cut the name to
    "Authentication disabl...".
  -->
</nav>

<style>
  /*
    The rail is a panel of its own rather than a column of links floating on
    the page: the section has a beginning, its groups, and the person it is
    about. The width keeps the links at the measure they had before the card's
    padding took some of it back.
  */
  .rail {
    position: sticky;
    top: calc(var(--z-topbar-height) + var(--z-space-6));
    flex: none;
    width: calc(var(--z-settings-rail-width) + var(--z-space-6));
    max-height: calc(100dvh - (var(--z-topbar-height) + var(--z-space-6)) - var(--z-space-6));
    overflow-y: auto;
    padding: var(--z-space-3);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-lg);
    background: var(--z-surface);
  }
  .rail-title {
    margin: 0 0 var(--z-space-3);
    padding: 0 var(--z-space-2);
    font-size: var(--z-text-2xs);
    line-height: var(--z-leading-2xs);
    font-weight: var(--z-weight-semibold);
    letter-spacing: var(--z-tracking-wide);
    text-transform: uppercase;
    color: var(--z-text-muted);
  }
  .group + .group {
    margin-top: var(--z-space-4);
  }
  .group-label {
    margin: 0 0 var(--z-space-1);
    padding: 0 var(--z-space-2);
    font-size: var(--z-text-2xs);
    line-height: var(--z-leading-2xs);
    font-weight: var(--z-weight-medium);
    letter-spacing: var(--z-tracking-wide);
    text-transform: uppercase;
    color: var(--z-text-subtle);
  }
  ul {
    display: flex;
    flex-direction: column;
    gap: var(--z-nudge-2);
    margin: 0;
    padding: 0;
    list-style: none;
  }
  a {
    display: flex;
    align-items: center;
    gap: var(--z-space-3);
    height: var(--z-space-8);
    padding: 0 var(--z-space-2);
    border-radius: var(--z-radius-md);
    color: var(--z-text-muted);
    text-decoration: none;
    font-size: var(--z-text-sm);
    font-weight: var(--z-weight-medium);
    white-space: nowrap;
    transition:
      background-color var(--z-motion-fast) var(--z-ease),
      color var(--z-motion-fast) var(--z-ease);
  }
  a:hover {
    background: var(--z-surface-hover);
    color: var(--z-text);
  }
  a.current {
    background: var(--z-accent-subtle);
    color: var(--z-accent);
  }
  a.locked {
    color: var(--z-text-subtle);
  }
  .label {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  a :global(.lock) {
    flex: none;
  }
  .note {
    margin: var(--z-space-4) 0 0;
    padding: var(--z-space-3);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-md);
    background: var(--z-surface-sunken);
    font-size: var(--z-text-2xs);
    line-height: var(--z-leading-2xs);
    color: var(--z-text-muted);
  }
  /*
    A tablet has the sidebar collapsed to icons and still not enough width for
    a rail beside a table, so the same list runs across the top of the page
    instead and scrolls sideways. The group names go: seven labelled chips in a
    row are their own map.
  */
  @media (max-width: 1180px) {
    .rail {
      /* Relative, not static: the group labels below are absolutely positioned
         to be hidden, and only a positioned rail is the containing block that
         keeps them inside its own scroll area. Static lets them escape it and
         widen the page. */
      position: relative;
      /* The sticky offset above belongs to the vertical rail. Left on, it is
         honoured by a relatively positioned strip too, and moved the tabs down
         over the page's own heading. */
      top: auto;
      display: flex;
      align-items: flex-start;
      gap: var(--z-space-2);
      width: auto;
      max-width: 100%;
      max-height: none;
      overflow-x: auto;
      /* The strip is navigation, not a panel: the card around the rail has no
         meaning when the same list runs across the page. */
      padding: 0 0 var(--z-space-1);
      border: 0;
      background: none;
    }
    .rail-title {
      display: none;
    }
    .group + .group {
      margin-top: 0;
    }
    .group-label {
      position: absolute;
      width: 1px;
      height: 1px;
      overflow: hidden;
      clip: rect(0 0 0 0);
      white-space: nowrap;
    }
    .group + .group ul {
      padding-left: var(--z-space-2);
      border-left: var(--z-border-width) solid var(--z-border);
    }
    ul {
      flex-direction: row;
    }
    a {
      height: var(--z-space-8);
      padding: 0 var(--z-space-3);
      border: var(--z-border-width) solid var(--z-border);
      background: var(--z-surface);
    }
    a.current {
      border-color: var(--z-accent-border);
    }
    .note {
      display: none;
    }
  }
</style>
