<!--
  Repair settings: the account behind a person's repairs, the repositories Eli
  may help, and the form that links an account. It was a fold-out in the middle
  of the repairs page, and is a page of its own so the hub is about repairs and
  this is about whether they can happen.
-->
<script lang="ts">
  import Button from '$lib/components/Button.svelte';
  import PageHeader from '$lib/components/PageHeader.svelte';
  import SectionNav from '$lib/components/SectionNav.svelte';
  import { router } from '$lib/router';
  import { session } from '$lib/state/session.svelte';
  import RepairSetupPanel from '$lib/repairs/RepairSetupPanel.svelte';

  let panel = $state<RepairSetupPanel | null>(null);
  const admin = $derived(session.can('admin'));
  const navItems = $derived([
    { id: 'access', label: 'Account' },
    ...(admin
      ? [
          { id: 'repositories', label: 'Repositories' },
          { id: 'link-account', label: 'People', secondary: true },
        ]
      : []),
  ]);
</script>

<PageHeader
  title="Repair settings"
  subtitle="Link your GitHub account, and choose which repositories Eli may repair and when."
  breadcrumb={[{ label: 'Eli repairs', href: '/repairs' }, { label: 'Settings' }]}
  onrefresh={() => panel?.reload()}
>
  <Button href="/settings/assistant">Model providers</Button>
</PageHeader>

<SectionNav items={navItems} label="On this page" />

<RepairSetupPanel bind:this={panel} initialRepo={router.query.get('repo') ?? ''} />
