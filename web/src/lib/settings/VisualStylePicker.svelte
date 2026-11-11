<script lang="ts">
  import { Check } from '@lucide/svelte';
  import { visualStyle } from '$lib/state/ui-style.svelte';
  import { UI_STYLE_OPTIONS, type UIStyle } from '$lib/state/ui-style';
  import Button from '$lib/components/Button.svelte';
  import Logo from '$lib/components/Logo.svelte';
  let announcement = $state('');
  function select(value: UIStyle): void {
    visualStyle.set(value);
    announcement = `${UI_STYLE_OPTIONS.find((option) => option.value === value)?.label} selected for this browser.`;
  }
</script>

<fieldset>
  <legend>Visual style</legend>
  <p class="description">The palette and type size. Your other choices stay as you set them.</p>
  <div class="choices">
    {#each UI_STYLE_OPTIONS as option (option.value)}
      <label class="choice" class:selected={visualStyle.choice === option.value}>
        <input
          type="radio"
          name="ui-style"
          value={option.value}
          checked={visualStyle.choice === option.value}
          onchange={() => select(option.value)}
          aria-describedby="style-description-{option.value}"
        />
        <span class="title"
          ><span>{option.label}</span>{#if visualStyle.choice === option.value}<Check
              size={16}
              aria-hidden="true"
            />{/if}</span
        >
        <span class="preview" data-style={option.value} aria-hidden="true">
          <span class="preview-head"><Logo variant="mark" size={24} label="" /> Zoomies</span>
          <span class="preview-panel"
            ><span>Running jobs</span><strong class="tabular">6</strong></span
          >
        </span>
        <span class="description" id="style-description-{option.value}">{option.description}</span>
      </label>
    {/each}
  </div>
  <Button variant="ghost" onclick={() => select('mission-control')}
    >Reset visual style to Mission Control</Button
  >
  <span class="sr-only" aria-live="polite" aria-atomic="true">{announcement}</span>
</fieldset>

<style>
  fieldset {
    min-width: 0;
    margin: 0;
    border: 0;
    padding: var(--z-space-4) var(--z-space-5);
    border-bottom: var(--z-border-width) solid var(--z-border);
  }
  legend {
    float: left;
    width: 100%;
    padding: 0;
    margin-bottom: var(--z-space-2);
    font-size: var(--z-text-base);
    font-weight: var(--z-weight-semibold);
  }
  .description {
    display: block;
    margin: 0;
    color: var(--z-text-muted);
    font-size: var(--z-text-xs);
    line-height: var(--z-leading-xs);
  }
  .choices {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: var(--z-space-4);
    margin-block: var(--z-space-4);
    clear: both;
  }
  .choice {
    position: relative;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: var(--z-space-3);
    padding: var(--z-space-4);
    border: var(--z-border-width-thick) solid var(--z-border-strong);
    border-radius: var(--z-radius-md);
    cursor: pointer;
  }
  .choice:hover {
    background: var(--z-surface-hover);
  }
  .choice.selected {
    border-color: var(--z-accent);
  }
  .choice:has(input:focus-visible) {
    box-shadow: var(--z-focus-ring);
  }
  input {
    position: absolute;
    opacity: 0;
    width: var(--z-control-box);
    height: var(--z-control-box);
  }
  .title {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--z-space-2);
    font-weight: var(--z-weight-semibold);
    font-size: var(--z-text-sm);
  }
  .preview {
    display: flex;
    flex-direction: column;
    justify-content: center;
    gap: var(--z-space-2);
    min-height: var(--z-style-preview-height);
    padding: var(--z-space-3);
    border-radius: var(--z-radius-sm);
    background: var(--z-classic-preview-bg);
    color: var(--z-classic-preview-text);
  }
  .preview-head {
    display: flex;
    align-items: center;
    gap: var(--z-space-2);
    font-weight: var(--z-weight-semibold);
    font-size: var(--z-text-sm);
  }
  .preview-panel {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--z-space-2);
    padding: var(--z-space-2) var(--z-space-3);
    border-radius: var(--z-radius-sm);
    background: var(--z-classic-preview-surface);
    font-size: var(--z-text-xs);
  }
  .preview-panel strong {
    color: var(--z-classic-preview-accent);
    font-size: var(--z-text-lg);
  }
  .preview[data-style='mission-control'] {
    background: var(--z-mission-bg);
    color: var(--z-mission-text);
  }
  .preview[data-style='mission-control'] .preview-panel {
    background: var(--z-mission-surface);
  }
  .preview[data-style='mission-control'] strong {
    color: var(--z-mission-accent);
  }
  @media (max-width: 768px) {
    .choices {
      grid-template-columns: minmax(0, 1fr);
    }
  }
</style>
