<!--
  Your own account.

  Small, and first in the section, because of one specific moment: an
  administrator resets somebody's password, the shell tells them to change it
  in Settings, and this is what they have to find when they get here. When that
  is the case the card says so and the button is the primary one on the page.

  Refreshing asks who we are again, which is how a role granted a minute ago
  starts to apply without signing out and in.
-->
<script lang="ts">
  import { KeyRound, LogOut } from '@lucide/svelte';
  import { ApiError, logoutOthers } from '$lib/api/client';
  import { MIN_PASSWORD_LENGTH } from '$lib/passwords';
  import { roleDescription, roleLabel } from '$lib/roles';
  import { session } from '$lib/state/session.svelte';
  import { toasts } from '$lib/state/toasts.svelte';
  import Badge from '$lib/components/Badge.svelte';
  import Button from '$lib/components/Button.svelte';
  import ConfirmDialog from '$lib/components/ConfirmDialog.svelte';
  import Dialog from '$lib/components/Dialog.svelte';
  import Field from '$lib/components/Field.svelte';
  import Input from '$lib/components/Input.svelte';
  import PageHeader from '$lib/components/PageHeader.svelte';
  import TwoStepPanel from '$lib/twostep/TwoStepPanel.svelte';

  let open = $state(false);
  let current = $state('');
  let next = $state('');
  let repeat = $state('');
  let saving = $state(false);
  let errors = $state<Record<string, string>>({});

  const lengthError = $derived(
    next.length === 0 || next.length >= MIN_PASSWORD_LENGTH
      ? ''
      : `At least ${MIN_PASSWORD_LENGTH} characters. This one has ${next.length}.`,
  );
  const matchError = $derived(repeat.length > 0 && repeat !== next ? 'These do not match.' : '');
  const ready = $derived(next.length >= MIN_PASSWORD_LENGTH && repeat === next);

  /** The first letter of the name, for the avatar. */
  const initial = $derived(session.displayName.trim().charAt(0).toUpperCase() || '?');
  /** What the role may do, in the words the Users page uses. */
  const roleDetail = $derived(roleDescription(session.role));

  function start(): void {
    current = '';
    next = '';
    repeat = '';
    errors = {};
    open = true;
  }

  let othersOpen = $state(false);

  async function signOutOthers(): Promise<boolean> {
    try {
      const result = await logoutOthers();
      const n = result.mcp_connections_ended ?? 0;
      toasts.success(
        'Signed out everywhere else',
        n === 0
          ? 'Every other session signed in as you has ended.'
          : `Every other session has ended, and ${n === 1 ? '1 MCP connection' : `${n} MCP connections`} with them.`,
      );
      return true;
    } catch (cause) {
      toasts.fromError(cause, 'Your other sessions were not signed out');
      return false;
    }
  }

  async function change(): Promise<void> {
    if (!ready) return;
    saving = true;
    errors = {};
    try {
      await session.changePassword(current || undefined, next);
      toasts.success(
        'Password changed',
        'Every other session signed in as you has been signed out, and your MCP connections ended.',
      );
      open = false;
    } catch (cause) {
      if (cause instanceof ApiError) errors = cause.fieldErrors();
      toasts.fromError(cause, 'That password was not changed');
    } finally {
      saving = false;
    }
  }
</script>

<PageHeader
  title="Account"
  subtitle="Who you are signed in as, your password, and two-step verification."
  onrefresh={() => session.refresh()}
/>

<section class="profile" class:urgent={session.mustChangePassword} aria-labelledby="account-name">
  <span class="avatar" aria-hidden="true">{initial}</span>
  <div class="identity">
    <h2 id="account-name" class="name">Signed in as {session.displayName}</h2>
    <div class="meta">
      {#if session.role}
        <Badge tone="neutral" label={roleLabel(session.role)} size="sm" dot={false} />
      {/if}
      {#if session.authDisabled}
        <Badge tone="pending" label="Authentication off" size="sm" dot={false} />
      {/if}
      {#if session.mustChangePassword}
        <Badge tone="danger" label="Change required" size="sm" dot={false} />
      {/if}
    </div>
    <p class="detail">
      {#if session.authDisabled}
        Every request is treated as an administrator, because authentication is switched off in the
        configuration.
      {:else if session.mustChangePassword}
        Your password was set by somebody else. Choose your own in the Password card below.
      {:else}
        {roleDetail || roleLabel(session.role)}
      {/if}
    </p>
  </div>
</section>

{#if !session.authDisabled}
  <div class="security">
    <section class="card" aria-labelledby="password-heading">
      <span class="icon" aria-hidden="true"><KeyRound size={18} /></span>
      <div class="body">
        <h3 id="password-heading" class="title">Password</h3>
        <p class="detail">
          {#if session.mustChangePassword}
            Set by somebody else. Choose your own now.
          {:else}
            Changing it ends every other session signed in as you, and every MCP connection.
          {/if}
        </p>
      </div>
      <div class="actions">
        <Button
          variant={session.mustChangePassword ? 'primary' : 'secondary'}
          icon={KeyRound}
          onclick={start}
        >
          Change password
        </Button>
      </div>
    </section>

    {#if session.identity?.kind === 'user'}
      <section class="card" aria-labelledby="sessions-heading">
        <span class="icon" aria-hidden="true"><LogOut size={18} /></span>
        <div class="body">
          <h3 id="sessions-heading" class="title">Sessions</h3>
          <p class="detail">
            Sign out every other browser signed in as you. MCP connections end with them and have to
            be approved again.
          </p>
        </div>
        <div class="actions">
          <Button icon={LogOut} onclick={() => (othersOpen = true)}>Sign out other sessions</Button>
        </div>
      </section>

      <TwoStepPanel />
    {/if}
  </div>
{/if}

<ConfirmDialog
  bind:open={othersOpen}
  title="Sign out other sessions"
  description="Every other browser signed in as you is signed out. This one stays signed in."
  consequences={[
    'Every MCP connection you have made, Claude, or any other client, ends too, and has to be approved again.',
  ]}
  confirmLabel="Sign out the others"
  onconfirm={signOutOthers}
/>

<Dialog
  bind:open
  title="Change your password"
  description="Every other session signed in as you is ended, and every MCP connection."
  size="sm"
>
  <form
    id="change-password-form"
    class="form"
    novalidate
    onsubmit={(event) => {
      event.preventDefault();
      void change();
    }}
  >
    <Field
      label="Current password"
      hint="Leave empty if an administrator just reset it for you."
      error={errors.old_password}
    >
      {#snippet children({ id, describedBy, invalid })}
        <Input
          bind:value={current}
          {id}
          {describedBy}
          {invalid}
          type="password"
          autocomplete="current-password"
        />
      {/snippet}
    </Field>

    <Field
      label="New password"
      hint="At least {MIN_PASSWORD_LENGTH} characters."
      error={errors.new_password ?? lengthError}
    >
      {#snippet children({ id, describedBy, invalid })}
        <Input
          bind:value={next}
          {id}
          {describedBy}
          invalid={invalid || Boolean(lengthError)}
          type="password"
          autocomplete="new-password"
        />
      {/snippet}
    </Field>

    <Field label="New password again" error={matchError}>
      {#snippet children({ id, describedBy, invalid })}
        <Input
          bind:value={repeat}
          {id}
          {describedBy}
          invalid={invalid || Boolean(matchError)}
          type="password"
          autocomplete="new-password"
        />
      {/snippet}
    </Field>
  </form>

  {#snippet footer()}
    <Button variant="ghost" onclick={() => (open = false)}>Cancel</Button>
    <Button
      variant="primary"
      type="submit"
      form="change-password-form"
      loading={saving}
      disabled={!ready}
    >
      Change password
    </Button>
  {/snippet}
</Dialog>

<style>
  /* The person, first: a larger card than the actions under it, so the page
     answers "whose account is this?" before it offers to change anything. */
  .profile {
    display: flex;
    align-items: center;
    gap: var(--z-space-4);
    padding: var(--z-space-5);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-lg);
    background: var(--z-surface);
  }
  .profile.urgent {
    border-color: var(--z-pending-border);
    background: var(--z-pending-subtle);
  }
  .avatar {
    display: inline-grid;
    place-items: center;
    flex: none;
    width: var(--z-space-12);
    height: var(--z-space-12);
    border-radius: var(--z-radius-full);
    background: var(--z-accent-subtle);
    color: var(--z-accent);
    font-size: var(--z-text-lg);
    font-weight: var(--z-weight-semibold);
  }
  .identity {
    flex: 1 1 18rem;
    min-width: 0;
  }
  .name {
    margin: 0;
    font-size: var(--z-text-lg);
    line-height: var(--z-leading-lg);
    font-weight: var(--z-weight-semibold);
    color: var(--z-text);
    overflow-wrap: anywhere;
  }
  .meta {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: var(--z-space-2);
    margin-top: var(--z-space-2);
  }
  .detail {
    margin: var(--z-nudge-2) 0 0;
    font-size: var(--z-text-xs);
    line-height: var(--z-leading-xs);
    color: var(--z-text-muted);
  }
  .security {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-4);
    margin-top: var(--z-space-4);
  }
  /* The same card TwoStepPanel draws, so the three security actions read as
     one group rather than three slightly different panels. */
  .card {
    display: flex;
    align-items: flex-start;
    flex-wrap: wrap;
    gap: var(--z-space-4);
    padding: var(--z-space-4) var(--z-space-5);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-md);
    background: var(--z-surface);
  }
  .icon {
    display: inline-grid;
    place-items: center;
    flex: none;
    width: var(--z-space-10);
    height: var(--z-space-10);
    border-radius: var(--z-radius-full);
    background: var(--z-accent-subtle);
    color: var(--z-accent);
  }
  .body {
    flex: 1 1 16rem;
    min-width: 0;
  }
  .title {
    margin: 0;
    font-size: var(--z-text-base);
    line-height: var(--z-leading-base);
    font-weight: var(--z-weight-semibold);
    color: var(--z-text);
  }
  .actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--z-space-2);
  }
  .form {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-4);
    padding-bottom: var(--z-space-2);
  }
</style>
