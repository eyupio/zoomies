<!--
  How Eli answers this person: how long, and for whom. The default is the
  answer that fits on a phone; someone who wants the reasons, or the engineer's
  version, says so here once and every question after carries it. It is the
  reader's choice, so it sits with the personal providers and is kept with the
  account, not on a provider.
-->
<script lang="ts">
  import Segmented from '$lib/components/Segmented.svelte';
  import {
    ELI_LENGTH_OPTIONS,
    ELI_TONE_OPTIONS,
    type EliLength,
    type EliTone,
  } from '$lib/state/eli-style';
  import { prefs } from '$lib/state/prefs.svelte';
</script>

<section class="style" aria-labelledby="assistant-style">
  <h3 id="assistant-style">How Eli answers</h3>
  <div class="setting">
    <div class="text">
      <p class="label">Length</p>
      <p class="description">
        Short, the default, is the answer and what to do, under about two hundred words. Medium adds
        the reasons and the evidence. Long is the full picture, including what Eli ruled out. Eli
        gives more when you ask for it, whatever this says.
      </p>
    </div>
    <Segmented
      options={ELI_LENGTH_OPTIONS}
      value={prefs.eliStyle.length}
      label="Answer length"
      onchange={(value) => (prefs.eliStyle = { ...prefs.eliStyle, length: value as EliLength })}
    />
  </div>
  <div class="setting">
    <div class="text">
      <p class="label">Voice</p>
      <p class="description">
        Plain explains terms and gives no commands unless asked. Balanced, the default, uses plain
        words with the exact names of the settings, codes and commands to act on. Technical is for
        an engineer: exact names and figures, common terms not explained.
      </p>
    </div>
    <Segmented
      options={ELI_TONE_OPTIONS}
      value={prefs.eliStyle.tone}
      label="Answer voice"
      onchange={(value) => (prefs.eliStyle = { ...prefs.eliStyle, tone: value as EliTone })}
    />
  </div>
</section>

<style>
  .style {
    display: grid;
    gap: var(--z-space-4);
    margin-bottom: var(--z-space-6);
    padding: var(--z-space-4);
    background: var(--z-surface);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-lg);
  }
  h3 {
    margin: 0;
    font-size: var(--z-text-base);
    font-weight: var(--z-weight-semibold);
  }
  .setting {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: var(--z-space-3) var(--z-space-4);
  }
  .text {
    flex: 1;
    min-width: 16rem;
  }
  .label,
  .description {
    margin: 0;
  }
  .label {
    font-size: var(--z-text-sm);
    font-weight: var(--z-weight-medium);
  }
  .description {
    margin-top: var(--z-space-1);
    font-size: var(--z-text-sm);
    color: var(--z-text-muted);
  }
</style>
