<!--
  The layers over a page: the detail drawer, the destructive confirmation and
  the rich tooltip. Each is opened by a real control so focus, Escape and the
  focus trap are exercised with the real behaviour rather than a static panel.
-->
<script lang="ts">
  import { CircleCheck, TriangleAlert } from '@lucide/svelte';
  import Button from '$lib/components/Button.svelte';
  import ConfirmDialog from '$lib/components/ConfirmDialog.svelte';
  import Drawer from '$lib/components/Drawer.svelte';
  import Panel from '$lib/components/Panel.svelte';
  import Popover from '$lib/components/Popover.svelte';
  import Tooltip from '$lib/components/Tooltip.svelte';

  let drawerOpen = $state(false);
  let confirmOpen = $state(false);
  let popoverAnchor = $state<HTMLElement | null>(null);
</script>

<Panel
  title="Overlays"
  description="Focus is trapped while open and returns to the trigger when the layer closes."
>
  <div class="row">
    <Button onclick={() => (drawerOpen = true)}>Open the runner drawer</Button>
    <Button variant="danger" onclick={() => (confirmOpen = true)}>Delete a pool</Button>
    <Button onclick={(event) => (popoverAnchor = event.currentTarget as HTMLElement)}>
      Open an anchored panel
    </Button>
    <Tooltip
      title="Why this runner is draining"
      text="The pool is above its ceiling, so this runner finishes its job and exits."
      wide
    >
      <Button>Hover or focus for the reason</Button>
      {#snippet content()}
        <dl class="card">
          <dt>State</dt>
          <dd>Draining since 4 minutes ago</dd>
          <dt>Current job</dt>
          <dd>build (ubuntu-24.04)</dd>
          <dt>Next step</dt>
          <dd>Nothing: the runner leaves when the job finishes.</dd>
        </dl>
      {/snippet}
    </Tooltip>
  </div>
  <p class="note">
    The drawer is reachable from every row's Inspect action. On a phone it keeps a strip of the page
    visible down its left edge, so it reads as a panel over the page rather than a new page. The
    anchored panel is the one a band opens: it stays in its owner's accessibility tree while the
    browser draws it over everything, and Tab leaving it closes it.
  </p>
</Panel>

<Popover
  open={popoverAnchor !== null}
  anchor={popoverAnchor}
  label="An anchored panel"
  onclose={() => (popoverAnchor = null)}
>
  <p class="popover-copy">
    A panel that belongs to the control that opened it: placed against it, flipped above it when
    there is no room below, and closed when it scrolls away.
  </p>
  {#snippet footer()}
    <Button size="sm" variant="ghost" onclick={() => (popoverAnchor = null)}>Cancel</Button>
    <Button size="sm" variant="primary" onclick={() => (popoverAnchor = null)}>Apply</Button>
  {/snippet}
</Popover>

<Drawer
  bind:open={drawerOpen}
  title="run_demo00"
  description="linux-x64 · host zoomies-12vcpu"
  width="lg"
>
  <div class="drawer-body">
    <p class="eyebrow">State</p>
    <p>Busy, running build (ubuntu-24.04) for 1 minute 40 seconds.</p>
    <p class="eyebrow">Recent output</p>
    <pre>docker buildx build --platform linux/amd64 .</pre>
    <p class="eyebrow">Actions</p>
    <div class="row">
      <Button>Drain runner</Button>
      <Button variant="ghost">Copy the runner id</Button>
      <Button
        variant="danger"
        disabled
        lock
        reason="Only administrators can remove a runner. Ask an administrator for the administrator role."
      >
        Remove
      </Button>
    </div>
  </div>
  {#snippet footer()}
    <Button variant="ghost" onclick={() => (drawerOpen = false)}>Close</Button>
  {/snippet}
</Drawer>

<ConfirmDialog
  bind:open={confirmOpen}
  title="Delete pool"
  name="linux-x64"
  description="This removes the pool from the scheduler. Runners already serving it are drained first."
  consequences={['3 runners will be drained', '2 queued jobs will be left for another pool']}
  requireName
  onconfirm={() => (confirmOpen = false)}
  oncancel={() => (confirmOpen = false)}
/>

<Panel title="Notices" description="A message that stays beside the thing it is about.">
  <div class="notices">
    <div class="notice ok">
      <CircleCheck size={16} aria-hidden="true" />
      <span>The pool was created paused. Resume it when its labels are ready.</span>
    </div>
    <div class="notice warn" role="status">
      <TriangleAlert size={16} aria-hidden="true" />
      <span>
        This controller is a development build. Update checks are off until a release is installed.
      </span>
    </div>
  </div>
</Panel>

<style>
  .row {
    display: flex;
    flex-wrap: wrap;
    gap: var(--z-space-3);
  }
  .note {
    max-width: 80ch;
    margin: var(--z-space-3) 0 0;
    font-size: var(--z-text-xs);
    color: var(--z-text-muted);
  }
  .popover-copy {
    margin: 0;
    font-size: var(--z-text-sm);
    line-height: var(--z-leading-sm);
    color: var(--z-text);
  }
  .card {
    display: grid;
    grid-template-columns: auto 1fr;
    gap: var(--z-space-1) var(--z-space-4);
    margin: 0;
  }
  .card dt {
    color: var(--z-text-muted);
  }
  .card dd {
    margin: 0;
    color: var(--z-text);
  }
  .drawer-body {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-2);
  }
  .drawer-body p {
    margin: 0;
  }
  .eyebrow {
    font-size: var(--z-text-xs);
    font-weight: var(--z-weight-semibold);
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: var(--z-text-muted);
  }
  pre {
    margin: 0;
    padding: var(--z-space-3);
    border-radius: var(--z-radius-sm);
    background: var(--z-surface-sunken);
    color: var(--z-text);
    font-family: var(--z-font-mono);
    font-size: var(--z-text-xs);
    overflow-x: auto;
  }
  .notices {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-3);
  }
  .notice {
    display: flex;
    align-items: flex-start;
    gap: var(--z-space-2);
    padding: var(--z-space-3) var(--z-space-4);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-sm);
    background: var(--z-surface);
    font-size: var(--z-text-sm);
  }
  .notice.ok {
    color: var(--z-text);
  }
  .notice.ok :global(svg) {
    color: var(--z-idle);
  }
  .notice.warn {
    color: var(--z-text);
    border-color: var(--z-pending-border);
    background: var(--z-pending-subtle);
  }
  .notice.warn :global(svg) {
    color: var(--z-pending);
  }
</style>
