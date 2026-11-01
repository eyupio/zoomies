# The shared look, carried into the product: specification

1 November 2026. This is the specification for implementing the look the
signed-in pack mocked, with what that look has gained since, in the product
itself. It follows [SPEC.md](SPEC.md) ("Direction chosen after pack 1") and
the signed-in pack's own closing note: build the kit once in `web/src/lib`,
then each page is a small change from its mock.

## Where the product is

The photographs taken on 1 November 2026 from the built controller (the demo
fleet and the first-run fixture, in Mission Control dark and Classic light at
1440 and 375 pixels) show that most of the pack's direction is already in the
product, and `docs/ui-guidelines.md` section 2 describes it: the sunken
sidebar with the blue rail, the page header with its section eyebrow, title
and short rule, cards with the extra-large radius on the page's own ground,
the Overview's stage hero, the band on every fleet page, and the public frame
with its stage, header and dark footer.

What the product does not have is what the console's look gained after the
pack was drawn, on 31 October 2026, and that is the gap this specification
closes:

| Element                                                                                         | In the product today                           | In the shared look                                                                              |
| ----------------------------------------------------------------------------------------------- | ---------------------------------------------- | ----------------------------------------------------------------------------------------------- |
| The primary button                                                                              | a rectangle in the darkened accent, white text | a pill in the brand's Runner Blue with Zoomies Black text, on every surface including the stage |
| Every other button                                                                              | a rectangle                                    | a pill; secondary outlined, ghost and danger unchanged in colour                                |
| A status pill (`Badge`)                                                                         | a small rectangle with the dot                 | a pill (full radius) with the dot                                                               |
| A command for the operator to run                                                               | a tinted box with a `<pre>` inside it          | a terminal window: the dark stage with a window bar, the command in mono and a copy control     |
| The form beside the stage (sign in, first account, consent)                                     | bare on the page                               | on a light card with the stage's radius and border                                              |
| A block of facts about one thing (who invited, as what, until when; a host's build and channel) | a tinted box                                   | the dark stage's tokens, as the band is, with the accent for the one figure that matters        |

Nothing else in the direction moves. Tokens, status colours, the band system,
the sidebar, the top bar, the footer, the log viewer and every data grid stay
as they are.

## What is produced

Two pull requests, in this order.

### 1. The kit, the shell and the public pages

Component changes in `web/src/lib/components/` and `web/src/lib/public/`, so
every page takes the look at once and no page is restyled by hand:

- **`Button`**: every variant is a pill (`--z-radius-full`); `primary` is
  `--z-brand-runner-blue` with `--z-brand-black` text and a brightness lift
  on hover, in both styles and both modes. Runner Blue under Zoomies Black is
  7.3:1, so it passes for text of any size where the darkened accent was
  needed only to carry white text (`docs/brand.md`, "Two decisions worth
  explaining"); the darkened accent stays for links, the active navigation
  entry and focus. The three sizes and `loading` are unchanged.
- **`IconButton`**: a pill, so the refresh control and the row actions match.
- **`Badge`**: a pill. Colour and shape still come from the state map; only the
  corners change.
- **`Input`, `Select`, `Textarea`**: the large radius (`--z-radius-lg`), so a
  field reads as the card's child rather than a box inside it. The small
  radius stays for checkboxes and the radio box.
- **`Terminal`**, new in `web/src/lib/components/`: a window bar with three dots
  and a title, a body in the mono face on the dark stage's tokens (remapped as
  the band remaps them, never a colour of its own), and a slot for the copy
  control and a line of fine print. It draws one or more commands as lines.
  It replaces the `.command` box in `AddHostFlow` (the install command, and
  the join command under the disclosure), the command in `HostDoctorCommand`,
  and the `runs-on` snippet the pool editor and the migration show. Nothing
  about what the commands say changes.
- **`Facts`**, new in `web/src/lib/components/`: a definition list on the dark
  stage's tokens, two columns on a desktop and one on a phone, with an
  optional pill beside a value. It replaces the tinted `version-match` block
  in `AddHostFlow` and is what the second pull request draws a runner's and a
  host's facts with.
- **`SplitPage`**: the column beside the stage is a light card with the stage's
  radius and the hairline border, on the page's own ground, with the same
  padding the stage has. The stage and the phone behaviour are unchanged.
- **The shell**: the sidebar, top bar and footer already carry the look. The
  only change is what the components above bring them: the pill on the band's
  Review button, the top bar's account control and the refresh button.
- **The gallery** (`web/src/gallery/`) gains the terminal and the facts panel
  in their states, so `make screenshots-gallery` photographs them.
- **Documentation**: `docs/ui-guidelines.md` section 1.4 (the radii line),
  section 3 (the `Button`, `IconButton`, `Badge`, `Input`, `Terminal` and
  `Facts` rows) and `docs/brand.md` (the sentence on the accent and buttons)
  say what the product now does; `docs/screenshots` are retaken with
  `make screenshots`.

### 2. The Overview, from its mock

A separate plan once the first pull request has landed, because the pull
request above changes how the page looks already and the plan should be
written against that. The mock's structure below the band: the four figures
as cards with a tinted icon tile each, the pools as cards with a pill and a
meter, Needs attention as a card of rows with a pill, a sentence and an Open
button, and Recent events as rows with a pill. The band keeps its figures,
its cuts and its switches, since the band system is documented and chosen.

## Constraints

- No raw colour and no raw value that has a token (`web/AGENTS.md`). No new
  token is needed: the pill is `--z-radius-full`, the terminal and the facts
  panel read the `--z-panel-*` tokens the band reads.
- No new dependency.
- Status colours keep their fixed mapping and are not used for decoration. A
  pill never carries a state by colour alone: the dot or the word is always
  there.
- Both styles (Mission Control and Classic) in both modes, at 1440, 375 and
  320 pixels, with no horizontal overflow and the contrast the pack's checker
  requires: 4.5:1 for text, 3:1 for a control's edge.
- Every command the product shows is the product's own string, unchanged.
- Identity stays: the logo, its backdrops, the dog, the paw.
- Prose in the repository's voice: British spelling, no em dash, no spaced
  double hyphen; commit messages are imperative sentences in plain prose.

## How it is checked

- `npm run lint`, `npm run check` and the unit tests in `web/unit/`.
- The Playwright suites that drive the public pages (`first-run.spec.ts`,
  `mcp-oauth.spec.ts`), the hosts pages (`connect.spec.ts`, `host-*.spec.ts`)
  and the accessibility pass (`a11y.spec.ts`), against the built binary.
- A new Playwright assertion that a primary button, a badge and the terminal
  carry the full radius and the stage's background, so a later component
  cannot drift back to a rectangle.
- The component gallery in all four style and mode combinations, by
  `gallery.spec.ts`.
- Photographs of the Overview, Pools, Hosts, Add a host (at the command step),
  the first account and sign in, at 1440 and 375 pixels in both styles, in the
  pull request.

## Not in scope

- Any page's structure beyond what the components change; the Overview is the
  second pull request and every other page follows it, one at a time.
- The band system, the sidebar's order, the top bar and the footer.
- A change to `tokens.css`.
- The status page's budget: it draws `ThemeSwitch` and `BrandStage` only, and
  neither changes.
