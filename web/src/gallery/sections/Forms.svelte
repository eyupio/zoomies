<!--
  Form controls and the states around them: hint, help, notice, validation
  error, disabled, read-only and refused. Every field is drawn with `Field`,
  which is the only layout form controls get, so the spacing and the
  `aria-describedby` wiring here are the product's own.
-->
<script lang="ts">
  import { Eye, EyeOff, Search } from '@lucide/svelte';
  import Button from '$lib/components/Button.svelte';
  import Checkbox from '$lib/components/Checkbox.svelte';
  import Field from '$lib/components/Field.svelte';
  import IconButton from '$lib/components/IconButton.svelte';
  import Input from '$lib/components/Input.svelte';
  import Panel from '$lib/components/Panel.svelte';
  import QuantityField from '$lib/components/QuantityField.svelte';
  import RadioGroup from '$lib/components/RadioGroup.svelte';
  import Select from '$lib/components/Select.svelte';
  import Slider from '$lib/components/Slider.svelte';
  import Switch from '$lib/components/Switch.svelte';
  import Textarea from '$lib/components/Textarea.svelte';

  let name = $state('linux-x64');
  let search = $state('');
  let secret = $state('hunter2-not-a-real-secret');
  let reveal = $state(false);
  let region = $state('');
  let notes = $state('');
  let checked = $state(true);
  let indeterminate = $state(true);
  let routing = $state('auto');
  let live = $state(true);
  let cpus = $state(4);
  let memory = $state(4096);

  const regions = [
    { value: 'eu-west', label: 'Europe (Ireland)' },
    { value: 'us-east', label: 'United States (Virginia)' },
    { value: 'ap-south', label: 'Asia Pacific (Mumbai)' },
    { value: 'retired', label: 'Asia Pacific (Osaka), retired', disabled: true },
  ];
  const cpuNotches = [1, 2, 4, 8, 16];
  /** A newline inside an attribute has to arrive through a value. */
  const labelsPlaceholder = 'linux\nx64';
</script>

<Panel title="Form controls" description="Field states, choice controls and the numeric field.">
  <div class="columns">
    <div class="column">
      <Field label="Pool name" hint="Lower-case letters, digits and dashes.">
        {#snippet children({ id, describedBy })}
          <Input bind:value={name} {id} {describedBy} placeholder="linux-x64" />
        {/snippet}
      </Field>

      <Field
        label="Webhook secret"
        help="GitHub shows this once, under the App's own settings, and never again."
      >
        {#snippet children({ id, describedBy })}
          <Input value="whsec_redacted_for_the_gallery" {id} {describedBy} mono />
        {/snippet}
      </Field>

      <Field
        label="Pool name"
        error="A pool with that name already exists. Pool names are unique across the controller."
      >
        {#snippet children({ id, describedBy, invalid })}
          <Input bind:value={name} {id} {describedBy} {invalid} />
        {/snippet}
      </Field>

      <Field label="Password" notice="Caps Lock is on." hint="At least twelve characters.">
        {#snippet children({ id, describedBy })}
          <Input bind:value={secret} {id} {describedBy} type={reveal ? 'text' : 'password'}>
            {#snippet trailing()}
              <IconButton
                icon={reveal ? EyeOff : Eye}
                label={reveal ? 'Hide the password' : 'Show the password'}
                size="sm"
                onclick={() => (reveal = !reveal)}
              />
            {/snippet}
          </Input>
        {/snippet}
      </Field>
    </div>

    <div class="column">
      <Field label="Search runners" hint="Matches a name, a pool or a host.">
        {#snippet children({ id, describedBy })}
          <Input bind:value={search} {id} {describedBy} type="search" icon={Search} />
        {/snippet}
      </Field>

      <Field label="Managed by" hint="Set in zoomies.yaml; the controller ignores changes here.">
        {#snippet children({ id, describedBy })}
          <Input value="the controller configuration" {id} {describedBy} disabled />
        {/snippet}
      </Field>

      <Field label="Runner ID" hint="Copied, never edited.">
        {#snippet children({ id, describedBy })}
          <Input value="run_demo00" {id} {describedBy} readonly mono spellcheck={false} />
        {/snippet}
      </Field>

      <Field label="Region" hint="Where GitHub's runners are registered.">
        {#snippet children({ id, describedBy, invalid })}
          <Select
            bind:value={region}
            options={regions}
            {id}
            {describedBy}
            {invalid}
            placeholder="Choose a region"
          />
        {/snippet}
      </Field>
    </div>
  </div>

  <div class="block">
    <h3>Choice controls</h3>
    <div class="columns">
      <div class="column choices">
        <Checkbox
          bind:checked
          label="Keep a runner warm"
          description="Idle runners stay registered until the pool's floor drops."
        />
        <Checkbox
          bind:checked={indeterminate}
          indeterminate
          label="All labels"
          description="Some of this pool's runners are excluded."
        />
        <Checkbox
          checked={false}
          disabled
          label="Send to a private host"
          description="No host is enrolled yet."
        />
        <Checkbox
          checked={false}
          disabled
          reason="Only administrators can change routing. Ask an administrator for the administrator role."
          label="Override routing"
          description="Kept reachable so the reason can be read."
        />
      </div>
      <div class="column choices">
        <Switch
          bind:checked={live}
          label="Live updates"
          description="Follow the event stream without refreshing."
        />
        <Switch
          checked={false}
          disabled
          label="Automatic updates"
          description="The update mode is manual."
        />
        <RadioGroup
          bind:value={routing}
          name="gallery-routing"
          legend="Routing"
          inline
          options={[
            { value: 'auto', label: 'Automatic', description: 'Match by label' },
            { value: 'manual', label: 'Manual', description: 'Assign hosts yourself' },
            { value: 'off', label: 'Off', disabled: true },
          ]}
        />
      </div>
    </div>
  </div>

  <div class="block">
    <h3>Numbers and quantities</h3>
    <div class="columns">
      <div class="column">
        <Slider
          values={cpuNotches}
          bind:value={cpus}
          label="CPU share"
          valuetext={(v) => `${v} ${v === 1 ? 'core' : 'cores'}`}
          marks={[
            { value: 4, label: 'recommended' },
            { value: 16, label: 'ceiling' },
          ]}
        />
        <Slider
          values={cpuNotches}
          value={4}
          label="CPU share, disabled"
          valuetext={(v) => `${v} cores`}
          disabled
        />
      </div>
      <div class="column">
        <Field label="Memory limit" hint="A runner larger than this is killed and retried once.">
          {#snippet children({ id, describedBy })}
            <QuantityField
              bind:value={memory}
              quantity="mb"
              label="Memory limit"
              {id}
              {describedBy}
              values={[1024, 2048, 4096, 8192, 16384]}
            />
          {/snippet}
        </Field>
      </div>
    </div>
  </div>

  <div class="block">
    <h3>Text area</h3>
    <Field label="Labels" hint="One per line. A workflow asks for all of them.">
      {#snippet children({ id, describedBy })}
        <Textarea
          bind:value={notes}
          {id}
          {describedBy}
          rows={3}
          placeholder={labelsPlaceholder}
          mono
        />
      {/snippet}
    </Field>
  </div>

  <div class="block">
    <h3>Buttons in a form row</h3>
    <p class="note">
      Save is primary and right-most; Cancel is beside it. The refused pair below cannot submit, and
      says why.
    </p>
    <div class="row">
      <Button variant="primary">Save pool</Button>
      <Button variant="ghost">Cancel</Button>
      <Button
        disabled
        reason="You are signed in as a viewer. Ask an administrator for the operator role."
        lock
      >
        Drain selected
      </Button>
    </div>
  </div>
</Panel>

<style>
  .columns {
    display: grid;
    /* min() rather than a bare 22rem: a phone is 375px wide, and a track with
       a 352px floor inside 16px of page padding and a panel's own padding
       pushed the document sideways. */
    grid-template-columns: repeat(auto-fit, minmax(min(22rem, 100%), 1fr));
    gap: var(--z-space-4) var(--z-space-6);
    align-items: start;
  }
  .column {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-4);
    min-width: 0;
  }
  .choices {
    gap: var(--z-space-3);
  }
  .block {
    margin-top: var(--z-space-5);
    padding-top: var(--z-space-4);
    border-top: var(--z-border-width) solid var(--z-border);
  }
  h3 {
    margin: 0 0 var(--z-space-3);
    font-size: var(--z-text-xs);
    font-weight: var(--z-weight-medium);
    color: var(--z-text-muted);
  }
  .note {
    max-width: 80ch;
    margin: 0 0 var(--z-space-3);
    font-size: var(--z-text-xs);
    color: var(--z-text-muted);
  }
  .row {
    display: flex;
    flex-wrap: wrap;
    gap: var(--z-space-3);
  }
</style>
