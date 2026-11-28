---
name: Zoomies
description: A self-hosted GitHub Actions runner fleet controller, presented as a calm operations console.
colors:
  brand-black: "#080808"
  brand-white: "#ffffff"
  brand-cool-grey: "#b9bcc2"
  brand-runner-blue: "#2f80ed"
  brand-fast-cyan: "#22d3ee"
  bg-light: "#f4f7fb"
  bg-dark: "#090e18"
  surface-light: "#ffffff"
  surface-dark: "#111b2b"
  raised-light: "#ffffff"
  raised-dark: "#19263b"
  sunken-light: "#eaf0f7"
  sunken-dark: "#0c1421"
  border-light: "#c3cede"
  border-dark: "#2b3a50"
  border-strong-light: "#697d96"
  border-strong-dark: "#7389a5"
  text-light: "#122235"
  text-dark: "#eef4fc"
  muted-light: "#405570"
  muted-dark: "#afbed2"
  subtle-light: "#536780"
  subtle-dark: "#9baec8"
  accent-light: "#1762bc"
  accent-dark: "#8ac2ff"
  accent-hover-light: "#12509e"
  accent-hover-dark: "#b0d7ff"
  accent-subtle-light: "#e3edfc"
  accent-subtle-dark: "#142b47"
  accent-border-light: "#9ab8e2"
  accent-border-dark: "#55769c"
  accent-contrast-light: "#ffffff"
  accent-contrast-dark: "#07131d"
  idle-light: "#0f7a3d"
  idle-dark: "#5fd68c"
  busy-light: "#00757f"
  busy-dark: "#3bd6e8"
  pending-light: "#9a6100"
  pending-dark: "#f2b246"
  draining-light: "#4d3f5c"
  draining-dark: "#d0bee0"
  danger-light: "#bd2018"
  danger-dark: "#ff8d84"
  neutral-light: "#686d76"
  neutral-dark: "#7f858e"
typography:
  display:
    fontFamily: "InterVariable, Inter, Geist, ui-sans-serif, system-ui, sans-serif"
    fontSize: "36px"
    fontWeight: 500
    lineHeight: "42px"
    letterSpacing: "-0.01em"
  headline:
    fontFamily: "InterVariable, Inter, Geist, ui-sans-serif, system-ui, sans-serif"
    fontSize: "28px"
    fontWeight: 640
    lineHeight: "34px"
    letterSpacing: "-0.01em"
  title:
    fontFamily: "InterVariable, Inter, Geist, ui-sans-serif, system-ui, sans-serif"
    fontSize: "20px"
    fontWeight: 550
    lineHeight: "28px"
  body:
    fontFamily: "InterVariable, Inter, Geist, ui-sans-serif, system-ui, sans-serif"
    fontSize: "14px"
    fontWeight: 450
    lineHeight: "22px"
  label:
    fontFamily: "InterVariable, Inter, Geist, ui-sans-serif, system-ui, sans-serif"
    fontSize: "13px"
    fontWeight: 500
    lineHeight: "20px"
  mono:
    fontFamily: "JetBrains Mono, ui-monospace, SFMono-Regular, Menlo, Consolas, monospace"
    fontSize: "13px"
    fontWeight: 450
    lineHeight: "20px"
rounded:
  sm: "4px"
  md: "8px"
  lg: "12px"
  xl: "14px"
  full: "9999px"
spacing:
  space-1: "4px"
  space-2: "8px"
  space-3: "12px"
  space-4: "16px"
  space-5: "20px"
  space-6: "24px"
  space-8: "32px"
  space-10: "40px"
  space-12: "48px"
  space-16: "64px"
components:
  button-primary:
    backgroundColor: "{colors.brand-runner-blue}"
    textColor: "{colors.brand-black}"
    rounded: "{rounded.md}"
    padding: "0 12px"
    height: "32px"
  button-secondary:
    rounded: "{rounded.md}"
    padding: "0 12px"
    height: "32px"
  input:
    rounded: "{rounded.md}"
    padding: "0 12px"
    height: "32px"
  badge-status:
    rounded: "{rounded.sm}"
    padding: "0 8px"
    height: "20px"
  panel:
    rounded: "{rounded.xl}"
    padding: "16px 20px"
  metric-tile:
    rounded: "{rounded.xl}"
    padding: "16px"
  nav-item:
    rounded: "{rounded.md}"
    padding: "0 8px"
    height: "32px"
---

# Design System: Zoomies

## Overview

**Creative North Star: "The Kennel Club Mission Control"**

Zoomies is a watch room, not a dashboard. The operator leaves it open on a
second monitor through a working day, so the surface is quiet at rest,
unmistakable when something needs a person, and dense enough that a useful
number of runners fit without scrolling. The visual language is instrument
grade: Inter at operational sizes, a 4px grid, precise borders and restrained
cool shadows. The dog-park identity sits underneath that discipline as a warm
signature, in the mark, the artwork, the empty states and the vocabulary, never
as chrome on an operational row.

The default world is Mission Control: navy surfaces in dark mode, cool paper in
light mode, Runner Blue for interaction and Fast Cyan spent on the one thing
that is actually moving. Dark is the opening theme. A page opens with a black
band that states the answer before the evidence, then panels, grids and tiles
that carry the detail. Status is always carried by colour and shape together,
because an operator glancing from across the room must not need hue alone.

Refinement preserves this world. A redesign may replace the composition, but
keeps the product truth: every resource is visible without a refresh, every
status means one thing, and every number is tabular so columns agree.

**Key Characteristics:**
- Calm at rest, loud only where the fleet is wrong.
- Navy Mission Control surfaces, dark by default, with a fixed black page band.
- Dense Inter type on a strict 4px rhythm, never smaller than a person can read.
- Colour plus shape for every state; cyan exclusively means work in progress.
- Tonal layering and hairlines first, shadows only for overlays and state.
- Dog-park warmth in art and words, never in the operational grammar.

## Colors

A cool navy spine with one disciplined blue, one cyan reserved for running
work, and six fixed status hues that operators learn by position and shape as
much as by colour.

### Primary
- **Command Blue** (light `#1762bc`, dark `#8ac2ff`): the interactive accent.
  Links, primary focus, selected navigation, the active rail and every control
  an operator is meant to act on. It is a tuned Mission Control value rather
  than full-strength Runner Blue, so 13px text and filled-button labels clear
  their contrast floor in both themes.
- **Runner Blue** (`#2f80ed`): the brand constant. It fills primary buttons
  under Zoomies Black, the page band's accent line and the active navigation
  rail. It is an illustrative and identity value, not a text colour.

### Secondary
- **Fast Cyan** (light `#00757f`, dark `#3bd6e8`): the busy state, and only the
  busy state. It is the brand's "use sparingly" accent spent on the single most
  valuable glance signal on the page: a runner is executing a job right now.

### Tertiary
- **Signal Green** (light `#0f7a3d`, dark `#5fd68c`): idle, healthy, success.
- **Amber Beacon** (light `#9a6100`, dark `#f2b246`): provisioning,
  registering, pending, warning, throttled.
- **Cordon Plum** (light `#4d3f5c`, dark `#d0bee0`): draining, cordoned,
  paused, or waiting on a step somebody asked for.
- **Alarm Red** (light `#bd2018`, dark `#ff8d84`): failed, error, destructive.

### Neutral
- **Night Navy** (`#090e18` dark, `#f4f7fb` light): the page ground.
- **Deep Deck** (`#111b2b` dark, `#ffffff` light): panels, cards and table
  bodies.
- **Raised Deck** (`#19263b` dark, `#ffffff` light): dialogs, menus and the
  current navigation entry.
- **Sunken Well** (`#0c1421` dark, `#eaf0f7` light): table headers, code wells
  and the navigation column.
- **Hairline Navy** (light `#c3cede`, dark `#2b3a50`): the default border.
  **Strong Navy** (light `#697d96`, dark `#7389a5`) marks an input or the one
  container in focus.
- **Ink Navy** (light `#122235`) and **Frost Text** (`#eef4fc` dark) are primary
  text. **Muted Navy** and **Subtle Navy** step down for secondary labels and
  timestamps.

### The Classic alternate

Classic is the compact neutral identity, still selectable in Settings,
Appearance and kept for the public status page. Its light ground is `#f6f7f9`
with `#ffffff` surfaces, `#0b0c0e` text and `#1a63d8` accent; its dark ground
is `#0a0b0d` with `#131519` surfaces, `#f4f5f7` text and `#78b4ff` accent.
Classic sets body text at 13px/20px rather than Mission Control's 14px/22px.
Status meanings do not change between styles. Never mix a Classic surface value
into a Mission Control screen.

### Named Rules
**The Cyan Is Busy Rule.** Fast Cyan and its Mission Control value may only mean
"work is executing now". No note, badge, chart series or decorative flourish may
borrow it.

**The Six Mean Six Things Rule.** Idle, busy, pending, draining, danger and
neutral are a fixed vocabulary. They are never reused for categories, brands or
decoration, and each is also drawn as a distinct shape.

**The Information Is Not A Status Rule.** A note, hint or informational box uses
Command Blue and its subtle ground. It never wears one of the six status hues.

## Typography

**Display Font:** Inter Variable (with Geist and the system sans stack)
**Body Font:** Inter Variable (same stack)
**Label/Mono Font:** JetBrains Mono (with the system mono stack)

**Character:** An instrument face with a soft landing. Four non-round axis
weights (450, 500, 550, 640) give Inter enough voice for dense controls without
a second family; JetBrains Mono marks anything an operator may copy, compare or
read line by line. Both are self-hosted woff2, so an air-gapped install never
waits for a font request.

### Hierarchy
- **Display** (500, 36px/42px, -0.01em): the page band and public stage
  headline. Hero figures share the size at 640.
- **Headline** (640, 28px/34px, -0.01em): overview metrics and compact band
  headlines.
- **Title** (550, 20px/28px): page titles. Panel headings sit one step down at
  16px/24px, weight 550.
- **Body** (450, 14px/22px): the default operator size in Mission Control.
  Prose in empty states and help steps up to 15px/24px. Classic is 13px/20px.
- **Label** (500, 13px/20px): badges, metadata, field labels. Sentence case,
  never tracked-out uppercase as a decorative label.
- **Mono** (450, 13px/20px): IDs, durations, commands, log output. Numerals in
  tables are tabular everywhere.

### Named Rules
**The Dense-But-Readable Rule.** Mission Control body text is 14px; Classic is
13px. Nothing an operator must read goes below 12px. Small caps are not a
substitute for hierarchy.

**The Absent Value Rule.** A missing value is two hyphens, written from the
`NO_VALUE` constant, never an em dash or an en dash. A formatter that has no
value returns the same mark, so a tile and the table beneath it agree.

**The Error Replaces The Hint Rule.** A field shows one message row. A
validation error takes the hint's place rather than adding a line, so nothing
below it moves under a pointer that is already on its way to the submit button.

## Layout

The shell is a fixed instrument frame. A 232px navigation column (56px when
collapsed) runs full height on the left, with a 52px top bar across the content
and a 1600px maximum content width. Panels and grids fill the width they are
given; the shell does not centre a narrow column on a wide monitor.

Space comes from a 4px base with only ten steps. Page gutters are 24px, card
padding 20px, table cells 12px vertical and 16px horizontal, and the gap between
form fields is 16px. Below the grid there are three optical nudges of 1px to 3px
for checkboxes and icon alignment; anything that can sit on the grid does.

Each page opens with a black **page band** above its evidence: an eyebrow, a
headline that says the answer ("9 things need you"), a short lede, optional
figures and, on the Overview, a picture. The band has four heights (Slim,
Compact, Standard, Expanded), chosen per page and remembered. A phone never
draws the stage; it keeps the figures as a plain panel so the records reach the
first screen.

Two breakpoints govern everything: 768px and 1180px. Under 768px the navigation
becomes a bottom bar with four watched sections and a **More** sheet, metric
tiles stack, every grid becomes cards read down, and form controls step to 16px
so iOS does not zoom the viewport. Between 768px and 1180px the navigation
starts collapsed. Above 1180px the full layout holds.

Settings uses a 200px rail, a 960px reading measure and a 1180px wide variant.
Public pages share a 1120px frame. Prose holds to a 72ch measure. Dialogs come
in 400/560/820px widths and drawers in 360/520/760px, cut to their content
rather than the viewport. Every overlay takes its width from the window, never
from a bare `100%`, so a page that overflows sideways cannot drag a modal off
screen.

## Elevation & Depth

Depth is mostly tonal. Panels are flat at rest, separated by hairlines and by
the difference between the page ground, the deck and the raised deck. Shadows
are cool-tinted and restrained; dark mode carries elevation mainly through
surface lightness rather than heavier shadow. A modal dims the page with the
same scrim everywhere: the page ground at 72% with a 3px blur, so a dialog over
a drawer dims by one amount, not two.

### Shadow Vocabulary
- **Hairline lift** (`0 1px 2px rgb(8 12 20 / 0.06)` light, `0 1px 2px rgb(0 0 0 / 0.5)` dark): the current navigation entry and small raised controls.
- **Menu lift** (`0 4px 12px -2px rgb(8 12 20 / 0.10)` light, `0 4px 12px -2px rgb(0 0 0 / 0.6)` dark): dropdowns, popovers and tooltips.
- **Overlay lift** (`0 16px 40px -8px rgb(8 12 20 / 0.16)` light, `0 16px 40px -8px rgb(0 0 0 / 0.7)` dark): dialogs and drawers.
- **Focus ring** (`0 0 0 2px var(--z-focus-gap), 0 0 0 4px var(--z-focus-colour)`): the box-shadow form of the one focus ring, for controls whose outline a clipping parent would cut.

### Named Rules
**The Flat-At-Rest Rule.** A card, panel or tile carries no shadow until state
asks for one. Hover changes surface or border, not height.

**The One Veil Rule.** Every modal uses the same scrim and blur from the token
file. A second dimming recipe is a bug.

## Shapes

Corners are gentle and functional, never pill-shaped for decoration. A 4px
radius belongs to checkboxes, chips and status badges; 8px to buttons, inputs,
selects and nav entries; 12px to dialogs, drawers, terminals and facts panels;
14px to cards, metric tiles, page bands and the public stage; 9999px only to
dots and circular indicators.

Border width is meaning, not taste. A 1px hairline separates things that belong
to the same list. A 2px edge marks the one selected, focused or wrong. A 3px
rail down the left edge flags a problem, a disabled pool or a failed step; it is
a signal rather than a boundary. The brand mark sits on a 22px inset radius
because that is the identity's own geometry; do not round it to the UI scale.

Drawn controls have their own sizes: a 15px checkbox box, a 14px switch thumb
and a 14px icon inside a button. Field and button heights stay on the spacing
scale: 24px small, 32px medium, 40px large (sign-in only). Under a coarse
pointer, touch controls take 44px.

### Named Rules
**The Border Weight Is Meaning Rule.** If two edges differ in thickness, they
differ in meaning. Do not add a third weight for looks.

**The Square-Shouldered Rule.** Pills are for dots and badges, not buttons,
cards or navigation. A card is a rounded rectangle, not a lozenge.

## Components

### Buttons
- **Shape:** medium radius (8px), 32px high, 12px horizontal padding, 8px gap
  between icon and label.
- **Primary:** Runner Blue ground with Zoomies Black text in both themes, so
  the label cannot be remapped by a dark band. Hover brightens by 12%, press
  darkens by 6%.
- **Secondary:** the current surface with a strong hairline and primary text.
  Hover steps to the hover surface, never to the accent.
- **Ghost:** transparent, muted text, hover surface and primary text.
- **Danger:** Alarm Red ground with the danger contrast text.
- **Focus:** the global 2px ring in Command Blue with a 2px gap. Never remove
  it; a border that shifts from grey to blue is not a focus indicator.
- **Loading:** the label is replaced by a spinner without changing the width,
  and the button stays focused with `aria-disabled` rather than `disabled`.

### Badges and Chips
- **Shape:** small radius (4px), 20px high, 8px padding. Status badges use the
  state's subtle ground, its border and its foreground, with a shape dot in the
  same colour.
- **Rule:** colour and shape always travel together. A badge that could appear
  in a legend must survive greyscale.

### Cards and Panels
- **Corner:** 14px. **Border:** 1px hairline. **Background:** deep deck.
- **Internal padding:** header 16px by 20px, body 20px. A list body may run
  flush so the card is one frame around full-width rows.
- **Header:** an optional eyebrow in 13px muted text, a 16px/24px semibold
  title, an optional one-line description and optional actions opposite.

### Inputs and Fields
- **Shape:** 8px radius, 32px high, 12px padding, 1px strong hairline.
- **Focus:** the global Command Blue ring; the border also steps to the accent
  where a component draws its own. Hover darkens the hairline to the subtle
  text colour. Invalid uses Alarm Red.
- **Phone:** all input, textarea and select glyphs step to 16px below 768px.
  The sign-in form's large controls also take a 44px touch height.

### Navigation
- **Style:** a sunken column, 232px wide, with 32px entries at 8px radius. Each
  entry is 14px/500 Inter in muted text with a 14px icon.
- **States:** hover is the hover surface with primary text; the current page is
  the raised surface, semibold, with a 2px inset Runner Blue rail and a hairline
  lift. The phone replaces the column with a bottom bar and a More sheet, never
  with a collapsed column.

### Page band (signature)
- **Shape:** a black stage in both themes, 14px radius, 40px block padding and
  48px inline padding, with a hairline in the panel border colour.
- **Content:** eyebrow, headline (36px/42px, weight 500), a 40px by 2px Runner
  Blue rule, a muted lede limited to 28rem, optional actions and a strip of
  figures. The accent line inside the headline is Runner Blue.
- **Behaviour:** it states the page's answer before its evidence and can be
  hidden per page; the figures then fall back to a plain panel. It never uses a
  status colour, so it cannot be wrong in either theme.

### Metric tile
- **Shape:** 14px radius, 16px padding, 1px hairline, deep deck ground.
- **Content:** a 13px/500 muted label, a 28px/34px bold tabular value with a
  small unit, then a wrapping foot with a delta, a one-line hint and an inline
  sparkline. Hover is for the linked variant only: a border step and the hover
  surface, never a lift.

## Do's and Don'ts

### Do:
- **Do** take every colour, radius, space and duration from the token file; a
  value used twice belongs there.
- **Do** pair every status colour with its shape, and keep Fast Cyan for
  running work alone.
- **Do** keep the page band black in both themes, and keep its accent and lede
  legible on that fixed ground.
- **Do** use tabular numerals in every table and figure column.
- **Do** keep a dense 32px control rhythm on desktop and step to touch sizes
  only under a coarse pointer or the phone breakpoint.
- **Do** let a grid fill its frame, sharing spare width among columns the
  operator has not sized.
- **Do** use the dog artwork in the brand moments, empty states and first run,
  where it carries warmth without competing with the fleet.

### Don't:
- **Don't** introduce glows, gradient text, glassmorphism, decorative sparkles,
  oversized rounded cards or charts that exist to fill space.
- **Don't** reuse a status hue for a category, a chart series or a note.
- **Don't** write a raw colour or a one-off radius in a component.
- **Don't** make a mascot part of an operational row, a table cell or a control.
- **Don't** animate layout or reflow a table while an operator is reading it;
  animate colour, the dot and the panel, at 120ms to 320ms.
- **Don't** remove the focus ring or replace it with a colour change.
- **Don't** write an em dash or an en dash as the absent value; use the
  `NO_VALUE` mark.
