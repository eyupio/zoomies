<!--
  The copies that leave the machine.

  A backup beside the database is a backup against a mistake; this section is
  the other half, and it exists on the page so that "is the offsite copy
  actually there?" is a question with an answer rather than an assumption. That
  is why the listing is fetched live from the bucket when it is opened, and why
  a fleet with no remote configured is told so in as many words instead of
  being shown a tidy table and left to draw its own conclusion.

  Fetching a copy brings it back into the backup directory as an ordinary
  backup. It deliberately does not restore: that is the table above, staged and
  confirmed by name, after somebody has seen the copy land.
-->
<script lang="ts">
  import { Cloud, CloudOff, CloudUpload, Download, Trash2 } from '@lucide/svelte';
  import {
    checkBackupRemote,
    deleteRemoteBackup,
    fetchRemoteBackup,
    listRemoteBackups,
    shipBackups,
  } from '$lib/api/client';
  import type { BackupRemote, RemoteBackupCopy } from '$lib/api/types';
  import { formatBytes, pluralise } from '$lib/format';
  import { toasts } from '$lib/state/toasts.svelte';
  import Badge from '$lib/components/Badge.svelte';
  import Button from '$lib/components/Button.svelte';
  import ConfirmDialog from '$lib/components/ConfirmDialog.svelte';
  import RelativeTime from '$lib/components/RelativeTime.svelte';
  import Tooltip from '$lib/components/Tooltip.svelte';

  interface Props {
    remotes: readonly BackupRemote[];
    /** A copy that came back is a new row in the table above. */
    onchanged: () => void;
    disabled?: boolean;
  }

  let { remotes, onchanged, disabled = false }: Props = $props();

  let shipping = $state(false);
  let checking = $state<string | null>(null);
  let open = $state<string | null>(null);
  let copies = $state<readonly RemoteBackupCopy[]>([]);
  let listing = $state(false);
  let listError = $state<string | null>(null);
  let fetching = $state<string | null>(null);

  let removing = $state<RemoteBackupCopy | null>(null);
  let removeOpen = $state(false);
  let removeBusy = $state(false);

  async function ship(): Promise<void> {
    shipping = true;
    try {
      const result = await shipBackups();
      if (result.error) {
        // Partly worked is the honest answer: one bucket refusing must not
        // read as none of them having been sent.
        toasts.warning(
          result.sent.length > 0 ? 'Some copies went' : 'Nothing was copied',
          result.error,
        );
      } else if (result.sent.length === 0) {
        toasts.success('Already offsite', 'Every remote holds every backup it is meant to.');
      } else {
        toasts.success(
          'Copied offsite',
          `${pluralise(result.sent.length, 'copy', 'copies')} sent.`,
        );
      }
      if (open) await load(open);
      onchanged();
    } catch (cause) {
      toasts.fromError(cause, 'The backups were not copied');
    } finally {
      shipping = false;
    }
  }

  async function check(remote: BackupRemote): Promise<void> {
    checking = remote.name;
    try {
      const result = await checkBackupRemote(remote.name);
      if (result.ok) {
        toasts.success(`${remote.name} answers`, `${result.where} is reachable and writable.`);
      } else {
        toasts.error(`${remote.name} refused`, result.error ?? 'No reason was given.');
      }
    } catch (cause) {
      toasts.fromError(cause, `${remote.name} could not be tested`);
    } finally {
      checking = null;
    }
  }

  async function load(name: string): Promise<void> {
    listing = true;
    listError = null;
    try {
      const result = await listRemoteBackups(name);
      copies = result.items;
    } catch (cause) {
      copies = [];
      listError = cause instanceof Error ? cause.message : String(cause);
    } finally {
      listing = false;
    }
  }

  async function toggle(remote: BackupRemote): Promise<void> {
    if (open === remote.name) {
      open = null;
      copies = [];
      return;
    }
    open = remote.name;
    copies = [];
    await load(remote.name);
  }

  async function bringBack(remote: string, copy: RemoteBackupCopy): Promise<void> {
    fetching = copy.id;
    try {
      const backup = await fetchRemoteBackup(remote, copy.id, {});
      toasts.success(
        'Brought back',
        `${backup.id} is in the backup directory, verified. Restoring it is the table above.`,
      );
      onchanged();
    } catch (cause) {
      toasts.fromError(cause, 'That copy did not come back');
    } finally {
      fetching = null;
    }
  }

  function askRemove(copy: RemoteBackupCopy): void {
    removing = copy;
    removeOpen = true;
  }

  async function remove(): Promise<boolean> {
    if (!removing) return true;
    removeBusy = true;
    try {
      await deleteRemoteBackup(removing.remote, removing.id);
      toasts.success('Removed', `${removing.id} is no longer in ${removing.remote}.`);
      if (open) await load(open);
      return true;
    } catch (cause) {
      toasts.fromError(cause, 'That copy was not removed');
      return false;
    } finally {
      removeBusy = false;
    }
  }
</script>

<section class="remotes" aria-labelledby="backup-remotes">
  <header>
    <div>
      <h3 id="backup-remotes">Copies off this machine</h3>
      <p>
        {#if remotes.length === 0}
          Nothing leaves this host. A backup beside the database survives a mistake, not the disk —
          add a destination under <code>backup.remotes</code> in <code>zoomies.yaml</code>, or keep
          shipping the directory yourself.
        {:else}
          Every backup is copied to {pluralise(remotes.length, 'destination', 'destinations')} after it
          is taken, and again on the hour if one was unreachable.
        {/if}
      </p>
    </div>
    {#if remotes.length > 0}
      <Button variant="secondary" icon={CloudUpload} onclick={ship} loading={shipping} {disabled}>
        Copy offsite now
      </Button>
    {/if}
  </header>

  {#if remotes.length === 0}
    <p class="none"><CloudOff size={16} aria-hidden="true" /> No backup remotes are configured.</p>
  {:else}
    <ul class="list">
      {#each remotes as remote (remote.name)}
        <li class:failing={!!remote.last_error}>
          <div class="row">
            <div class="who">
              <span class="name">
                <Cloud size={15} aria-hidden="true" />
                {remote.name}
                {#if remote.disabled}
                  <Badge tone="neutral" label="Disabled" size="sm" dot={false} />
                {/if}
                {#if remote.encrypted}
                  <Tooltip
                    text="The archive is sealed with a passphrase before it leaves this host."
                  >
                    <Badge tone="idle" label="Encrypted" size="sm" dot={false} />
                  </Tooltip>
                {:else}
                  <Tooltip
                    text="The archive is uploaded as it is. Anyone who can read the bucket can open the whole fleet: set a passphrase on the remote."
                  >
                    <Badge tone="pending" label="Plain" size="sm" dot={false} />
                  </Tooltip>
                {/if}
              </span>
              <span class="second mono">{remote.where}</span>
              <span class="second">
                {remote.endpoint}
                {#if remote.keep > 0}
                  · keeps {pluralise(remote.keep, 'copy', 'copies')}
                {/if}
              </span>
            </div>
            <div class="state">
              {#if remote.uploading}
                <span class="second">Sending…</span>
              {:else if remote.last_upload_at}
                <span class="second">
                  last copy <RelativeTime value={remote.last_upload_at} plain />
                </span>
              {:else}
                <span class="second unset">nothing sent yet</span>
              {/if}
              {#if remote.listed_at}
                <span class="second">
                  {pluralise(remote.copies, 'copy', 'copies')} · {formatBytes(remote.bytes)}
                </span>
              {/if}
            </div>
            <div class="actions">
              <Button
                variant="ghost"
                size="sm"
                onclick={() => check(remote)}
                loading={checking === remote.name}
                disabled={disabled || remote.disabled}
              >
                Test
              </Button>
              <Button
                variant="ghost"
                size="sm"
                onclick={() => toggle(remote)}
                disabled={disabled || remote.disabled}
              >
                {open === remote.name ? 'Hide copies' : 'Show copies'}
              </Button>
            </div>
          </div>

          {#if remote.last_error}
            <p class="why">{remote.last_error}</p>
          {/if}

          {#if open === remote.name}
            <div class="copies">
              {#if listing}
                <p class="second">Reading {remote.where}…</p>
              {:else if listError}
                <p class="why">{listError}</p>
              {:else if copies.length === 0}
                <p class="second">This remote holds no backups of this fleet.</p>
              {:else}
                <ul>
                  {#each copies as copy (copy.key)}
                    <li>
                      <span class="mono">{copy.id}</span>
                      <span class="second">
                        taken <RelativeTime value={copy.taken_at} plain /> · {formatBytes(
                          copy.bytes,
                        )}{copy.encrypted ? ' · encrypted' : ''}
                      </span>
                      <span class="copy-actions">
                        <Button
                          variant="ghost"
                          size="sm"
                          icon={Download}
                          onclick={() => bringBack(remote.name, copy)}
                          loading={fetching === copy.id}
                          {disabled}
                        >
                          Bring back
                        </Button>
                        <Button
                          variant="danger"
                          size="sm"
                          icon={Trash2}
                          onclick={() => askRemove(copy)}
                          {disabled}
                        >
                          Remove
                        </Button>
                      </span>
                    </li>
                  {/each}
                </ul>
              {/if}
            </div>
          {/if}
        </li>
      {/each}
    </ul>
  {/if}
</section>

<ConfirmDialog
  bind:open={removeOpen}
  title="Remove the offsite copy"
  name={removing?.id ?? ''}
  description={`This deletes ${removing?.id ?? 'the copy'} from ${removing?.remote ?? 'the remote'}. It is the copy that exists because the ones on this host might not.`}
  confirmLabel="Remove it"
  tone="danger"
  requireName
  busy={removeBusy}
  onconfirm={remove}
/>

<style>
  .remotes {
    padding: var(--z-space-4) var(--z-space-5);
    border-bottom: var(--z-border-width) solid var(--z-border);
  }
  header {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: var(--z-space-4);
    flex-wrap: wrap;
  }
  h3 {
    margin: 0;
    font-size: var(--z-text-sm);
    font-weight: var(--z-weight-semibold);
    color: var(--z-text);
  }
  header p {
    margin: var(--z-space-1) 0 0;
    max-width: 80ch;
    font-size: var(--z-text-xs);
    line-height: var(--z-leading-xs);
    color: var(--z-text-muted);
  }
  .none {
    display: flex;
    align-items: center;
    gap: var(--z-space-2);
    margin: var(--z-space-3) 0 0;
    font-size: var(--z-text-xs);
    color: var(--z-text-muted);
  }
  .list {
    list-style: none;
    margin: var(--z-space-3) 0 0;
    padding: 0;
    display: grid;
    gap: var(--z-space-2);
  }
  .list > li {
    padding: var(--z-space-3);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-sm);
    background: var(--z-surface-raised);
  }
  .list > li.failing {
    box-shadow: inset var(--z-nudge-1) 0 0 0 var(--z-danger);
  }
  .row {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: var(--z-space-4);
    flex-wrap: wrap;
  }
  .who {
    min-width: 0;
    display: grid;
    gap: var(--z-space-1);
  }
  .name {
    display: flex;
    align-items: center;
    gap: var(--z-space-2);
    font-size: var(--z-text-sm);
    font-weight: var(--z-weight-medium);
    color: var(--z-text);
  }
  .state {
    display: grid;
    gap: var(--z-space-1);
    text-align: right;
  }
  .actions,
  .copy-actions {
    display: flex;
    align-items: center;
    gap: var(--z-space-2);
  }
  .second {
    font-size: var(--z-text-xs);
    color: var(--z-text-muted);
  }
  .unset {
    font-style: italic;
  }
  .mono {
    font-family: var(--z-font-mono);
    font-size: var(--z-text-xs);
    color: var(--z-text);
    overflow-wrap: anywhere;
  }
  .why {
    margin: var(--z-space-2) 0 0;
    font-size: var(--z-text-xs);
    line-height: var(--z-leading-xs);
    color: var(--z-danger);
    overflow-wrap: anywhere;
  }
  .copies {
    margin-top: var(--z-space-3);
    padding-top: var(--z-space-3);
    border-top: var(--z-border-width) solid var(--z-border);
  }
  .copies ul {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    gap: var(--z-space-2);
  }
  .copies li {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--z-space-3);
    flex-wrap: wrap;
  }
  .copies p {
    margin: 0;
  }

  @media (max-width: 40rem) {
    .state {
      text-align: left;
    }
  }
</style>
