<!--
  Adding a provider.

  A page rather than a dialog, for AddHostFlow's reason: the credential is
  typed here, and a dialog closes when a hand slips. The form itself lives in
  $lib/providers so that editing one, on the provider's own page, is the same
  five steps rather than a second form that drifts away from this one.
-->
<script lang="ts">
  import type { Provider } from '$lib/api/types';
  import { router } from '$lib/router';
  import { session } from '$lib/state/session.svelte';
  import ErrorState from '$lib/components/ErrorState.svelte';
  import PageHeader from '$lib/components/PageHeader.svelte';
  import ProviderForm from '$lib/providers/ProviderForm.svelte';

  const canAdmin = $derived(session.can('admin'));

  function cancel(): void {
    router.navigate('/providers');
  }

  function done(provider: Provider): void {
    router.navigate(provider.id ? `/providers/${provider.id}` : '/providers');
  }
</script>

<PageHeader
  title="Add a provider"
  breadcrumb={[{ label: 'Providers', href: '/providers' }, { label: 'Add a provider' }]}
  subtitle="Five steps: where it is and how we sign in, where a machine is built, what shape it is, what it may spend, and what the controller makes of it. Nothing is rented until you raise the ceiling above zero."
/>

{#if canAdmin}
  <ProviderForm oncancel={cancel} ondone={done} />
{:else}
  <ErrorState
    title="Not allowed"
    description="Adding a provider needs the administrator role, because it stores a credential that can create and destroy machines. An administrator can grant it under Settings, or add the provider for you."
  />
{/if}
