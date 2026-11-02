<!--
  The development-only component gallery.

  N-T4 in roadmap/archive/ui-audit-2026-10-01.md asks for the states the supplied screenshots never show
  (disabled permissions, a failed fetch, a disconnected stream, an empty
  filtered list, keyboard focus) to be reviewable in one place, in both styles
  and both colour modes. This is that place. It renders the real components
  from `$lib/components`; it is not a second set of primitives and must never
  become one.

  It is deliberately its own document (see gallery.html): `vite build` never
  builds it, the binary never embeds it, the router does not know it exists.
  Run `make gallery`, or `npm run dev` and open
  http://127.0.0.1:5173/gallery.html?style=mission-control&mode=dark.
-->
<script lang="ts">
  import { THEME_OPTIONS } from '$lib/state/theme.svelte';
  import Segmented from '$lib/components/Segmented.svelte';
  import { resolveUIStyle, UI_STYLE_OPTIONS } from '$lib/state/ui-style';
  import { visualStyle } from '$lib/state/ui-style.svelte';
  import BandPicturesSection from './sections/BandPictures.svelte';
  import ControlsSection from './sections/Controls.svelte';
  import DataSection from './sections/Data.svelte';
  import EliRepairsSection from './sections/EliRepairs.svelte';
  import FeedbackSection from './sections/Feedback.svelte';
  import FormsSection from './sections/Forms.svelte';
  import OverlaysSection from './sections/Overlays.svelte';
  import PublicSection from './sections/Public.svelte';
  import SurfacesSection from './sections/Surfaces.svelte';
  import { applyChoice, choiceQuery, readChoice, resolveMode, type GalleryChoice } from './style';

  const initialChoice = readChoice(location.search);
  let choice = $state<GalleryChoice>(initialChoice);
  // The root attribute is what the tokens read, but a component that asks the
  // style store (a fresh grid's phone fallback) must see the same combination.
  visualStyle.preview(initialChoice.style);

  const styleOptions = UI_STYLE_OPTIONS.map((option) => ({
    value: option.value,
    label: option.label,
    name: option.description,
  }));

  function choose(next: Partial<GalleryChoice>): void {
    choice = { ...choice, ...next };
    applyChoice(choice);
    visualStyle.preview(choice.style);
    // replaceState rather than pushState: flicking between combinations while
    // reviewing must not fill the history with one entry per flick.
    history.replaceState(null, '', `${location.pathname}${choiceQuery(choice)}`);
  }
</script>

<main>
  <header>
    <div class="titles">
      <h1>Component gallery</h1>
      <p>
        Development only. Every control below is the component the product renders, shown in the
        states a healthy fleet never reaches. Nothing here is linked from the app.
      </p>
    </div>
    <div class="switcher">
      <Segmented
        label="Visual style"
        options={styleOptions}
        value={choice.style}
        onchange={(value) => choose({ style: resolveUIStyle(value) })}
      />
      <Segmented
        label="Colour mode"
        options={THEME_OPTIONS}
        value={choice.mode}
        onchange={(value) => choose({ mode: resolveMode(value) })}
      />
    </div>
  </header>

  <!--
    The assistant is not here on purpose. Its tokens are reviewed by the
    assistant captures in docs/screenshots, and its sheet's visual-viewport
    sizing, focus containment and close order need the real app shell and a
    conversation, which is what web/tests/assistant.spec.ts drives.
  -->

  <div class="grid">
    <ControlsSection />
    <FormsSection />
    <FeedbackSection />
    <SurfacesSection />
    <OverlaysSection />
    <DataSection />
    <EliRepairsSection />
    <PublicSection />
    <BandPicturesSection />
  </div>
</main>

<style>
  main {
    box-sizing: border-box;
    min-height: 100vh;
    padding: var(--z-space-6);
    background: var(--z-bg);
    color: var(--z-text);
  }
  header {
    display: flex;
    flex-wrap: wrap;
    align-items: flex-end;
    justify-content: space-between;
    gap: var(--z-space-4);
    max-width: 1440px;
    margin: 0 auto var(--z-space-6);
  }
  h1 {
    margin: 0;
    font-size: var(--z-text-2xl);
    font-weight: var(--z-weight-semibold);
  }
  .titles p {
    max-width: 60ch;
    margin: var(--z-space-1) 0 0;
    font-size: var(--z-text-sm);
    color: var(--z-text-muted);
  }
  .switcher {
    display: flex;
    flex-wrap: wrap;
    gap: var(--z-space-3);
  }
  /*
    One column, wide. The gallery is a review surface: a control that changes
    width between combinations would hide exactly the reflow the combination
    is being checked for.
  */
  .grid {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-5);
    max-width: 1440px;
    margin: 0 auto;
  }
  @media (max-width: 768px) {
    main {
      padding: var(--z-space-4);
    }
    header {
      align-items: stretch;
    }
    .switcher {
      flex-direction: column;
    }
  }
</style>
