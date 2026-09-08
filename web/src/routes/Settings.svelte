<!--
  Settings: accounts, tokens, appearance, configuration and what this instance is.

  Most of it needs the administrator role, and rather than hiding the tabs from
  everybody else they are disabled with the reason said once, above them: a
  viewer who cannot find the users page should learn why, not conclude the
  product does not have one.

  The tab lives in the URL, so a link to a particular tab works.
-->
<script lang="ts">
  import { Info, KeyRound, Palette, SlidersHorizontal, Users } from '@lucide/svelte';
  import { router } from '$lib/router';
  import { session } from '$lib/state/session.svelte';
  import PageHeader from '$lib/components/PageHeader.svelte';
  import Tabs from '$lib/components/Tabs.svelte';
  import type { TabItem } from '$lib/components/Tabs.svelte';
  import AboutPanel from '$lib/settings/AboutPanel.svelte';
  import AccountPanel from '$lib/settings/AccountPanel.svelte';
  import AppearancePanel from '$lib/settings/AppearancePanel.svelte';
  import ConfigurationPanel from '$lib/settings/ConfigurationPanel.svelte';
  import TokensPanel from '$lib/settings/TokensPanel.svelte';
  import UsersPanel from '$lib/settings/UsersPanel.svelte';

  const canAdmin = $derived(session.can('admin'));

  const tabs = $derived<TabItem[]>([
    { id: 'users', label: 'Users', icon: Users, disabled: !canAdmin },
    { id: 'tokens', label: 'API tokens', icon: KeyRound, disabled: !canAdmin },
    { id: 'appearance', label: 'Appearance', icon: Palette },
    { id: 'configuration', label: 'Configuration', icon: SlidersHorizontal, disabled: !canAdmin },
    { id: 'about', label: 'About', icon: Info },
  ]);

  /** The tab in the URL, falling back to one this operator is actually allowed to open. */
  const active = $derived.by(() => {
    const wanted = router.param('tab');
    const found = tabs.find((tab) => tab.id === wanted);
    if (found && !found.disabled) return found.id;
    return canAdmin ? 'users' : 'appearance';
  });

  function select(id: string): void {
    router.setQuery({ tab: id === (canAdmin ? 'users' : 'appearance') ? null : id });
  }

  /*
    Nothing on this page arrives over the stream: users, tokens and the
    configuration change when an administrator changes them, possibly in another
    browser. Refreshing re-reads whichever panel is open -- through a counter
    rather than by remounting it, so a half-typed form is not thrown away by
    somebody wanting to be sure the list is current -- and asks who we are
    again, which is how a role granted a minute ago starts to apply.
  */
  let reloadKey = $state(0);

  async function refreshPage(): Promise<void> {
    reloadKey += 1;
    await session.refresh();
  }
</script>

<PageHeader
  title="Settings"
  subtitle="Accounts, credentials, appearance and what this controller is running with."
  onrefresh={refreshPage}
/>

<div class="content">
  <AccountPanel />

  {#if !canAdmin}
    <p class="notice">
      Accounts, API tokens and the configuration need the administrator role. You are signed in as
      {session.role ?? 'a viewer'}, so those tabs are shown but not open to you.
    </p>
  {/if}

  <Tabs value={active} {tabs} label="Settings sections" onchange={select}>
    {#snippet children(current)}
      {#if current === 'users'}
        <UsersPanel {reloadKey} />
      {:else if current === 'tokens'}
        <TokensPanel {reloadKey} />
      {:else if current === 'appearance'}
        <AppearancePanel />
      {:else if current === 'configuration'}
        <ConfigurationPanel {reloadKey} />
      {:else}
        <AboutPanel {reloadKey} />
      {/if}
    {/snippet}
  </Tabs>
</div>

<style>
  .content {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-4);
  }
  .notice {
    margin: 0;
    max-width: 80ch;
    padding: var(--z-space-3) var(--z-space-4);
    border: 1px solid var(--z-border);
    border-radius: var(--z-radius-sm);
    background: var(--z-surface-sunken);
    font-size: var(--z-text-base);
    line-height: var(--z-leading-base);
    color: var(--z-text-muted);
  }
</style>
