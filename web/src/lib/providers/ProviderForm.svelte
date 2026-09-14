<!--
  Adding a provider, and editing one.

  The placement step is rendered from the schema the controller serves rather
  than written out here: a driver knows which of its settings exist, which are
  required and what choosing one means, and a form that carried its own copy
  would be wrong the first time a driver gained a setting. `SettingSpec.help`
  and `SettingSpec.consequence` are the driver's own sentences and are shown as
  written.

  Two things are borrowed from the pool wizard on purpose. Numbers are held as
  strings in the draft, because '' and '0' are different answers and
  `max_machines: 0` is the setting that rents nothing. And the server is asked
  for a verdict on the draft before anything is created, from the placement step
  onward, so the answer sits beside the setting that changes it while there is
  still a reason to change it.
-->
<script lang="ts">
  import { untrack } from 'svelte';
  import {
    ApiError,
    createProvider,
    listProviderKinds,
    updateProvider,
    validateProvider,
  } from '$lib/api/client';
  import type {
    Body,
    Provider,
    ProviderKind,
    ProviderSetting,
    ProviderValidation,
  } from '$lib/api/types';
  import { pluralise } from '$lib/format';
  import { severityStatus } from '$lib/status';
  import { toasts } from '$lib/state/toasts.svelte';
  import Badge from '$lib/components/Badge.svelte';
  import Checkbox from '$lib/components/Checkbox.svelte';
  import ErrorState from '$lib/components/ErrorState.svelte';
  import Field from '$lib/components/Field.svelte';
  import Input from '$lib/components/Input.svelte';
  import RemedyText from '$lib/components/RemedyText.svelte';
  import Select from '$lib/components/Select.svelte';
  import Switch from '$lib/components/Switch.svelte';
  import Textarea from '$lib/components/Textarea.svelte';
  import Wizard from '$lib/components/Wizard.svelte';
  import LabelMapEditor from '$lib/hosts/LabelMapEditor.svelte';
  import {
    applySettingDefaults,
    draftErrors,
    draftFromProvider,
    emptyDraft,
    FIELD_LABELS,
    stepForField,
    STEP_FIELDS,
    toProviderBody,
    willRentNothing,
    WIZARD_STEPS,
  } from './draft';
  import type { ProviderDraft } from './draft';

  interface Props {
    /** The provider being edited, or absent when one is being created. */
    provider?: Provider | null;
    oncancel: () => void;
    ondone: (provider: Provider) => void;
    class?: string;
  }

  let { provider = null, oncancel, ondone, class: className = '' }: Props = $props();

  const editing = $derived(Boolean(provider?.id));

  // The draft starts from the provider being edited and is the operator's
  // from then on, so this reads the prop once on purpose.
  let draft = $state<ProviderDraft>(
    untrack(() => (provider ? draftFromProvider(provider) : emptyDraft())),
  );
  let current = $state(0);
  let touched = $state<Record<string, boolean>>({});
  let serverErrors = $state<Record<string, string>>({});
  let submitting = $state(false);
  let panel = $state<HTMLDivElement | null>(null);

  let kinds = $state<ProviderKind[]>([]);
  let kindsError = $state<unknown>(null);
  let kindsAttempt = $state(0);

  let verdict = $state<ProviderValidation | null>(null);
  let validating = $state(false);

  const reviewStep = WIZARD_STEPS.length - 1;
  const placementStep = WIZARD_STEPS.findIndex((step) => step.id === 'placement');

  const kind = $derived(kinds.find((k) => k.kind === draft.kind) ?? null);
  const specs = $derived<ProviderSetting[]>(kind?.settings ?? []);
  const plainSpecs = $derived(specs.filter((spec) => !spec.advanced));
  const advancedSpecs = $derived(specs.filter((spec) => spec.advanced));

  const clientErrors = $derived(draftErrors(draft, specs, { editing }));
  const body = $derived(toProviderBody(draft));
  const nothingRented = $derived(willRentNothing(draft));

  /** Client rules show once a field has been left; server rules show at once. */
  const errors = $derived.by(() => {
    const out: Record<string, string> = { ...serverErrors };
    for (const [field, message] of Object.entries(clientErrors)) {
      if (touched[field]) out[field] = message;
    }
    return out;
  });

  const blocking = $derived.by(() => {
    const fields =
      current === reviewStep ? Object.keys(clientErrors) : (STEP_FIELDS[current] ?? []);
    return Object.entries(clientErrors)
      .filter(([field]) => fields.includes(field) || fields.includes(settingGroup(field)))
      .map(([, message]) => message);
  });
  const canAdvance = $derived(blocking.length === 0 && !submitting);

  /** A driver setting's step key: every `settings.<key>` belongs to placement. */
  function settingGroup(field: string): string {
    return field.startsWith('settings.') ? 'settings' : field;
  }

  function touch(field: string): void {
    touched = { ...touched, [field]: true };
    if (serverErrors[field] !== undefined) {
      const rest = { ...serverErrors };
      delete rest[field];
      serverErrors = rest;
    }
  }

  function touchStep(step: number): void {
    const fields = STEP_FIELDS[step] ?? [];
    const next = { ...touched };
    for (const field of Object.keys(clientErrors)) {
      if (fields.includes(settingGroup(field))) next[field] = true;
    }
    for (const field of fields) next[field] = true;
    touched = next;
  }

  function goTo(step: number): void {
    current = Math.min(Math.max(step, 0), reviewStep);
  }

  /* -- what the controller can build ---------------------------------------- */

  $effect(() => {
    void kindsAttempt;
    const controller = new AbortController();
    listProviderKinds(controller.signal)
      .then((response) => {
        kinds = response.items ?? [];
        kindsError = null;
      })
      .catch((cause: unknown) => {
        if (cause instanceof DOMException && cause.name === 'AbortError') return;
        kindsError = cause;
      });
    return () => controller.abort();
  });

  // One kind and nothing chosen is not a choice, so it is made. A form that
  // asks a question with one answer is a form with an extra click in it.
  $effect(() => {
    const only = kinds.length === 1 ? kinds[0] : undefined;
    if (!only || untrack(() => draft.kind) !== '') return;
    draft.kind = only.kind;
  });

  // Defaults the driver published, filled in as soon as its schema arrives and
  // never over an answer already given.
  $effect(() => {
    const list = specs;
    if (list.length === 0) return;
    const next = applySettingDefaults(
      untrack(() => draft),
      list,
    );
    if (next !== untrack(() => draft)) draft = next;
  });

  /* -- the server's verdict, before anything is created --------------------- */

  $effect(() => {
    if (current < placementStep) return;
    const payload = body;
    const controller = new AbortController();
    validating = true;
    const timer = setTimeout(() => {
      validateProvider(payload, provider?.id, controller.signal)
        .then((result) => {
          verdict = result;
        })
        .catch((cause: unknown) => {
          if (cause instanceof DOMException && cause.name === 'AbortError') return;
          // The verdict is guidance, not the gate. Losing it is not worth an
          // error state over a form the operator can still submit.
        })
        .finally(() => {
          validating = false;
        });
    }, 250);
    return () => {
      clearTimeout(timer);
      controller.abort();
    };
  });

  /* -- focus follows the step ------------------------------------------------ */

  let lastStep = -1;
  $effect(() => {
    const step = current;
    if (step === lastStep) return;
    const moved = lastStep !== -1;
    lastStep = step;
    if (moved) untrack(() => panel)?.focus();
  });

  /* -- submitting ------------------------------------------------------------ */

  function applyFieldErrors(cause: ApiError): void {
    const fields = cause.fieldErrors();
    serverErrors = fields;
    const first = Object.keys(fields)[0];
    if (first !== undefined) goTo(stepForField(first));
  }

  async function finish(): Promise<void> {
    if (submitting) return;
    touchStep(current);
    const outstanding = Object.keys(clientErrors);
    if (outstanding.length > 0) {
      const next = { ...touched };
      for (const field of outstanding) next[field] = true;
      touched = next;
      goTo(stepForField(outstanding[0] ?? 'name'));
      return;
    }
    submitting = true;
    try {
      const payload = toProviderBody(draft, { complete: editing });
      const saved =
        provider && provider.id
          ? await updateProvider(provider.id, payload as Body<'updateProvider'>)
          : await createProvider(payload);
      toasts.success(
        editing ? `Saved ${payload.name}` : `Added ${payload.name}`,
        (payload.max_machines ?? 0) === 0
          ? 'Nothing is rented yet: its ceiling is zero. Raise it when you are ready to spend.'
          : 'Run the check before the first machine is built.',
      );
      ondone(saved);
    } catch (cause) {
      if (cause instanceof ApiError) applyFieldErrors(cause);
      toasts.fromError(
        cause,
        editing ? 'The provider was not saved' : 'The provider was not added',
      );
    } finally {
      submitting = false;
    }
  }

  /* -- rendering one driver setting ------------------------------------------ */

  function settingValue(key: string): string {
    return draft.settings[key] ?? '';
  }

  function setSetting(key: string, value: string): void {
    draft.settings = { ...draft.settings, [key]: value };
  }

  const OS_OPTIONS = [
    { value: '', label: 'Any' },
    { value: 'ubuntu', label: 'Ubuntu' },
    { value: 'debian', label: 'Debian' },
    { value: 'fedora', label: 'Fedora' },
    { value: 'rocky', label: 'Rocky Linux' },
  ];
  const ARCH_OPTIONS = [
    { value: '', label: 'Any' },
    { value: 'amd64', label: 'amd64' },
    { value: 'arm64', label: 'arm64' },
  ];
  const BACKEND_OPTIONS = [
    { value: 'docker', label: 'Docker' },
    { value: 'podman', label: 'Podman' },
    { value: 'process', label: 'Process' },
  ];

  // The editor owns its rows from here: a row with an empty key is a row
  // half typed, which the map the draft holds cannot represent.
  let labelRows = $state(
    untrack(() => Object.entries(draft.machine_labels).map(([key, value]) => ({ key, value }))),
  );
  $effect(() => {
    const map: Record<string, string> = {};
    for (const row of labelRows) {
      if (row.key.trim() === '') continue;
      map[row.key.trim()] = row.value;
    }
    draft.machine_labels = map;
  });
</script>

{#snippet setting(spec: ProviderSetting)}
  <Field
    label={spec.label || spec.key}
    hint={spec.help}
    notice={spec.consequence}
    error={errors[`settings.${spec.key}`]}
    required={spec.required}
  >
    {#snippet children({ id, describedBy, invalid })}
      {#if spec.kind === 'bool'}
        <Checkbox
          checked={settingValue(spec.key) === 'true'}
          label={spec.label || spec.key}
          ariaLabel={spec.label || spec.key}
          onchange={(checked) => {
            setSetting(spec.key, checked ? 'true' : 'false');
            touch(`settings.${spec.key}`);
          }}
        />
      {:else}
        <!-- A function binding rather than a field: the answers live in one
             map keyed by the driver's own setting names, and a map cannot be
             bound to a control by path. -->
        <Input
          bind:value={() => settingValue(spec.key), (next) => setSetting(spec.key, next)}
          {id}
          {describedBy}
          {invalid}
          type={spec.kind === 'secret' ? 'password' : 'text'}
          inputmode={spec.kind === 'number' ? 'numeric' : undefined}
          mono={spec.kind === 'list'}
          placeholder={spec.default}
          onblur={() => touch(`settings.${spec.key}`)}
        />
      {/if}
    {/snippet}
  </Field>
{/snippet}

<Wizard
  class={className}
  steps={WIZARD_STEPS}
  bind:current
  {canAdvance}
  busy={submitting}
  finishLabel={editing ? 'Save changes' : 'Add provider'}
  cancelLabel="Cancel"
  onnext={() => touchStep(current)}
  onback={() => touchStep(current)}
  onfinish={finish}
  {oncancel}
>
  {#snippet children(step)}
    <div class="step" bind:this={panel} tabindex="-1" role="group" aria-label={step.title}>
      {#if step.id === 'connect'}
        {#if kindsError}
          <ErrorState
            error={kindsError}
            title="The provider kinds could not be read"
            onretry={() => (kindsAttempt += 1)}
          />
        {/if}
        <Field
          label="Kind"
          hint="Which infrastructure this rents machines from."
          error={errors.kind}
          required
        >
          {#snippet children({ id, describedBy, invalid })}
            <Select
              bind:value={draft.kind}
              {id}
              {describedBy}
              {invalid}
              placeholder="Choose one"
              options={kinds.map((k) => ({ value: k.kind, label: k.label || k.kind }))}
              onchange={() => touch('kind')}
            />
          {/snippet}
        </Field>

        <Field
          label="Name"
          hint="What you will call it on this page and in a log line."
          error={errors.name}
          required
        >
          {#snippet children({ id, describedBy, invalid })}
            <Input
              bind:value={draft.name}
              {id}
              {describedBy}
              {invalid}
              placeholder="proxmox-lab"
              onblur={() => touch('name')}
            />
          {/snippet}
        </Field>

        <Field
          label="Address"
          hint="Where this controller reaches it, scheme and port included."
          error={errors.endpoint}
          required
        >
          {#snippet children({ id, describedBy, invalid })}
            <Input
              bind:value={draft.endpoint}
              {id}
              {describedBy}
              {invalid}
              type="url"
              mono
              placeholder="https://pve.example.com:8006"
              onblur={() => touch('endpoint')}
            />
          {/snippet}
        </Field>

        <Field
          label="Credential"
          hint={editing
            ? 'Sealed in the database and never shown again. Leave this empty to keep the stored one.'
            : 'Sealed with the instance key. It is never returned, never logged, and never reaches a guest.'}
          error={errors.credential}
          required={!editing}
        >
          {#snippet children({ id, describedBy, invalid })}
            <Input
              bind:value={draft.credential}
              {id}
              {describedBy}
              {invalid}
              type="password"
              mono
              autocomplete="off"
              placeholder={editing ? 'Unchanged' : 'user@pve!token=uuid'}
              onblur={() => touch('credential')}
            />
          {/snippet}
        </Field>

        <Field
          label="Certificate authority"
          hint="The certificate to trust for this address. Leave empty to use the system trust store."
        >
          {#snippet children({ id, describedBy })}
            <Textarea
              bind:value={draft.ca_pem}
              {id}
              {describedBy}
              rows={3}
              placeholder="-----BEGIN CERTIFICATE-----"
            />
          {/snippet}
        </Field>

        <Switch
          bind:checked={draft.insecure_skip_verify}
          label="Do not verify the certificate"
          description="The credential then travels to whatever answers at that address. A homelab hypervisor's certificate is usually its own, which is why this exists rather than being refused — but pasting the certificate above is better."
        />
      {:else if step.id === 'placement'}
        {#if specs.length === 0}
          <p class="prose">
            This driver has no settings of its own. Everything it needs is on the other steps.
          </p>
        {:else}
          {#each plainSpecs as spec (spec.key)}
            {@render setting(spec)}
          {/each}
          {#if advancedSpecs.length > 0}
            <details class="advanced">
              <summary>Advanced settings ({advancedSpecs.length})</summary>
              <div class="advanced-body">
                {#each advancedSpecs as spec (spec.key)}
                  {@render setting(spec)}
                {/each}
              </div>
            </details>
          {/if}
        {/if}
      {:else if step.id === 'machine'}
        <p class="prose">
          One shape, and every machine this provider buys is it. A second shape is a second
          provider, which is also how two ceilings and two credentials are kept apart.
        </p>

        <Field
          label="Runner slots per machine"
          hint="How many runners one of these may carry at once."
          error={errors.machine_capacity}
          required
        >
          {#snippet children({ id, describedBy, invalid })}
            <Input
              bind:value={draft.machine_capacity}
              {id}
              {describedBy}
              {invalid}
              type="number"
              min={1}
              onblur={() => touch('machine_capacity')}
            />
          {/snippet}
        </Field>

        <Field label="Backend" hint="How runners are made on the machine once it is a host.">
          {#snippet children({ id, describedBy })}
            <Select
              bind:value={draft.machine_backend}
              {id}
              {describedBy}
              options={BACKEND_OPTIONS}
            />
          {/snippet}
        </Field>

        <div class="row">
          <Field label="vCPUs" hint="Empty means the template decides." error={errors.machine_cpus}>
            {#snippet children({ id, describedBy, invalid })}
              <Input
                bind:value={draft.machine_cpus}
                {id}
                {describedBy}
                {invalid}
                type="number"
                min={0}
                onblur={() => touch('machine_cpus')}
              />
            {/snippet}
          </Field>
          <Field label="Memory (MB)" error={errors.machine_memory_mb}>
            {#snippet children({ id, describedBy, invalid })}
              <Input
                bind:value={draft.machine_memory_mb}
                {id}
                {describedBy}
                {invalid}
                type="number"
                min={0}
                onblur={() => touch('machine_memory_mb')}
              />
            {/snippet}
          </Field>
          <Field label="Disk (MB)" error={errors.machine_disk_mb}>
            {#snippet children({ id, describedBy, invalid })}
              <Input
                bind:value={draft.machine_disk_mb}
                {id}
                {describedBy}
                {invalid}
                type="number"
                min={0}
                onblur={() => touch('machine_disk_mb')}
              />
            {/snippet}
          </Field>
        </div>

        <div class="row">
          <Field label="Operating system" hint="What a pool's platform will match against.">
            {#snippet children({ id, describedBy })}
              <Select bind:value={draft.machine_os} {id} {describedBy} options={OS_OPTIONS} />
            {/snippet}
          </Field>
          <Field label="Version">
            {#snippet children({ id, describedBy })}
              <Input bind:value={draft.machine_os_version} {id} {describedBy} placeholder="24.04" />
            {/snippet}
          </Field>
          <Field label="Architecture">
            {#snippet children({ id, describedBy })}
              <Select bind:value={draft.machine_arch} {id} {describedBy} options={ARCH_OPTIONS} />
            {/snippet}
          </Field>
        </div>

        <Field
          label="Machine labels"
          hint="What a host made from this machine answers to. Pools select hosts by these."
        >
          {#snippet children({ describedBy })}
            <LabelMapEditor bind:rows={labelRows} {describedBy} />
          {/snippet}
        </Field>
      {:else if step.id === 'limits'}
        <Field
          label="Maximum machines"
          hint="The ceiling. Zero rents nothing, which is what a provider starts at."
          error={errors.max_machines}
          required
        >
          {#snippet children({ id, describedBy, invalid })}
            <Input
              bind:value={draft.max_machines}
              {id}
              {describedBy}
              {invalid}
              type="number"
              min={0}
              onblur={() => touch('max_machines')}
            />
          {/snippet}
        </Field>

        <Field
          label="Machines built at once"
          hint="A hypervisor cloning four templates at once is slower at all four than it would have been at one."
          error={errors.max_creates_in_flight}
        >
          {#snippet children({ id, describedBy, invalid })}
            <Input
              bind:value={draft.max_creates_in_flight}
              {id}
              {describedBy}
              {invalid}
              type="number"
              min={1}
              onblur={() => touch('max_creates_in_flight')}
            />
          {/snippet}
        </Field>

        <Field
          label="Idle timeout"
          hint="How long a machine may sit with nothing to do before it is drained and deleted."
          error={errors.idle_timeout}
        >
          {#snippet children({ id, describedBy, invalid })}
            <Input
              bind:value={draft.idle_timeout}
              {id}
              {describedBy}
              {invalid}
              placeholder="15m"
              onblur={() => touch('idle_timeout')}
            />
          {/snippet}
        </Field>

        <Field
          label="Cost per machine-hour"
          hint="What one of these costs you, if you know. Shown on the machine page; nothing is decided by it."
          error={errors.cost_per_machine_hour}
        >
          {#snippet children({ id, describedBy, invalid })}
            <Input
              bind:value={draft.cost_per_machine_hour}
              {id}
              {describedBy}
              {invalid}
              type="number"
              min={0}
              onblur={() => touch('cost_per_machine_hour')}
            />
          {/snippet}
        </Field>

        <Switch
          bind:checked={draft.enabled}
          label="Enabled"
          description="A disabled provider builds nothing. What it already owns is still drained and deleted."
        />

        {#if nothingRented}
          <p class="note">{nothingRented}</p>
        {/if}
      {:else}
        <dl class="summary">
          <div>
            <dt>Kind</dt>
            <dd>{kind?.label || draft.kind || 'Not chosen'}</dd>
          </div>
          <div>
            <dt>Name</dt>
            <dd>{draft.name || 'Not set'}</dd>
          </div>
          <div>
            <dt>Address</dt>
            <dd class="mono">{draft.endpoint || 'Not set'}</dd>
          </div>
          <div>
            <dt>Machine</dt>
            <dd>
              {pluralise(Number(draft.machine_capacity) || 0, 'runner slot')} · {draft.machine_backend}
            </dd>
          </div>
          <div>
            <dt>Ceiling</dt>
            <dd>{draft.max_machines} machines</dd>
          </div>
        </dl>

        <section class="verdict" aria-live="polite" aria-label="What the controller makes of it">
          {#if validating && !verdict}
            <p class="note">Asking the controller…</p>
          {:else if verdict}
            {#if verdict.valid}
              <p class="note ok">
                The controller accepts this. Nothing has been created: adding it stores the
                configuration, and the check afterwards is what asks the provider itself.
              </p>
            {:else}
              <p class="note bad">
                {pluralise(verdict.errors?.length ?? 0, 'answer')} still
                {(verdict.errors?.length ?? 0) === 1 ? 'needs' : 'need'} fixing.
              </p>
              <ul class="rejected">
                {#each verdict.errors ?? [] as rejected (rejected.field)}
                  <li>
                    <!-- The API's own field name, in the words the form uses
                         for it. An operator should never have to work out that
                         "max_creates_in_flight" is the box they just filled. -->
                    <strong>{FIELD_LABELS[rejected.field ?? ''] ?? rejected.field}</strong>
                    {rejected.message}
                  </li>
                {/each}
              </ul>
            {/if}
            {#if (verdict.warnings?.length ?? 0) > 0}
              <ul class="warnings">
                {#each verdict.warnings ?? [] as warning (warning.code)}
                  <li>
                    <Badge status={severityStatus(warning.severity)} size="sm" />
                    <div>
                      <p class="title">{warning.title}</p>
                      {#if warning.detail}<p class="detail">
                          <RemedyText text={warning.detail} />
                        </p>{/if}
                    </div>
                  </li>
                {/each}
              </ul>
            {/if}
          {/if}
        </section>
      {/if}
    </div>
  {/snippet}
</Wizard>

<style>
  .step {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-4);
    outline: none;
  }
  .row {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: var(--z-space-3);
  }
  .prose {
    margin: 0;
    max-width: 70ch;
    font-size: var(--z-text-sm);
    line-height: var(--z-leading-sm);
    color: var(--z-text-muted);
  }
  .advanced {
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-sm);
    background: var(--z-surface-sunken);
  }
  .advanced summary {
    padding: var(--z-space-2) var(--z-space-3);
    cursor: pointer;
    font-size: var(--z-text-xs);
    font-weight: var(--z-weight-medium);
    color: var(--z-text-muted);
  }
  .advanced-body {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-4);
    padding: var(--z-space-3);
    border-top: var(--z-border-width) solid var(--z-border);
  }
  .note {
    margin: 0;
    padding: var(--z-space-2) var(--z-space-3);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-sm);
    background: var(--z-surface-sunken);
    font-size: var(--z-text-xs);
    line-height: var(--z-leading-xs);
    color: var(--z-text-muted);
  }
  .note.ok {
    border-color: var(--z-idle-border);
    background: var(--z-idle-subtle);
  }
  .note.bad {
    border-color: var(--z-danger-border);
    background: var(--z-danger-subtle);
  }
  .summary {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-2);
    margin: 0;
  }
  .summary div {
    display: grid;
    grid-template-columns: 12rem minmax(0, 1fr);
    gap: var(--z-space-3);
  }
  dt {
    font-size: var(--z-text-xs);
    color: var(--z-text-muted);
  }
  dd {
    margin: 0;
    font-size: var(--z-text-sm);
    color: var(--z-text);
    overflow-wrap: anywhere;
  }
  .verdict {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-3);
  }
  .rejected {
    display: flex;
    flex-direction: column;
    gap: var(--z-nudge-2);
    margin: 0;
    padding: 0 0 0 var(--z-space-4);
    font-size: var(--z-text-xs);
    line-height: var(--z-leading-xs);
    color: var(--z-text-muted);
  }
  .warnings {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-2);
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .warnings li {
    display: flex;
    align-items: flex-start;
    gap: var(--z-space-2);
  }
  .title {
    margin: 0;
    font-size: var(--z-text-sm);
    color: var(--z-text);
  }
  .detail {
    margin: var(--z-nudge-1) 0 0;
    font-size: var(--z-text-xs);
    line-height: var(--z-leading-xs);
    color: var(--z-text-muted);
  }
  @media (max-width: 768px) {
    .row {
      grid-template-columns: 1fr;
    }
    .summary div {
      grid-template-columns: 1fr;
      gap: var(--z-nudge-1);
    }
  }
</style>
