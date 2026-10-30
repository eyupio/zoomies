# UI audit: Eli repairs

Prepared 29 October 2026 against `250bd91c1`. The audit covers the four repair
pages, their entry points in Overview and the job drawer, and the shared shell
at desktop (1440 by 1000) and phone (375 by 812) widths. It was run against the
built UI with the repair API stubbed to a realistic world: one offer, one
running repair, one failed repair, three automatic repositories and a linked
account. The setup page was also read against a controller with demo
installation data.

This is a self-hosted product with no trial and no paywall, so "conversion"
here means the first repair started and the first repository enabled. The
review step before an attempt is spent is the equivalent of a purchase gate and
is treated as such.

Status: the first simplification pass addresses the critical items and several
of the high-impact items. The remaining findings are open.

What already works, and should survive a simplification:

* The case is shown before anything is spent.
* Reviewing readiness never calls a model and never spends an attempt.
* Starting a repair is one explicit press, and the promise that Eli pushes one
  commit and never merges is repeated where it matters.
* The attempt page leads with the outcome when the repair has stopped.
* History, offers and automatic repairs each have a real owner and a real job.

The problem is not that any one of those is wrong. It is that every one of them
is explained on the same screen, in the same voice, at the same volume.

## Critical

### 1. The hub says the same thing four times before the first action

**Pass:** Designer, First-time user

**Where:** `web/src/routes/EliRepairs.svelte:297-308` (header and band),
`:312-358` (Now, attention rails, setup bar), `:360-415` (section headings),
`web/src/lib/bands/lines.ts:177-195`, `web/src/lib/repairs/SetupBar.svelte:33-51`,
`web/src/lib/components/SectionNav.svelte`.

**Problem:** With one offer and one running repair, the page says so in a
compact band ("1 offer needs review, 1 repair is running"), in the section
navigation counts ("Needs you 1", "In progress 1"), in two attention rails
("1 offer needs review", "1 repair is running now"), in the section headings
("Needs you", "In progress") and in the setup bar. The first offer's button is
around 550px down at 1440 and around 600px down at 375. A first-time user has to
read four summaries and learn the word "offer" before reaching the one thing
they can do.

**Fix:** Remove `PageBand` and the attention rails from this route. Keep the
section navigation counts as the only summary, with `Needs you` first and open.
Render `SetupBar` only when something is actually missing, as one line with one
button; when setup is complete, omit it entirely. Move the primary call to
action into the header as "Fix a failed check", with "Paste a PR link" as a
secondary action inside that dialog.

### 2. The repair page's primary action is below the fold and behind an unrelated card

**Pass:** Designer, First-time user

**Where:** `web/src/routes/EliRepairNew.svelte:394-451`,
`web/src/lib/repairs/DecisionCard.svelte:148-188`,
`web/src/lib/repairs/AutomaticNote.svelte`, `web/src/lib/repairs/RepairSetup.svelte`.

**Problem:** At 375, "Fix with Eli" sits near 1000px down the page. Above it are
the full case card and a full-width "Nothing has started on its own for this
failure" note that only makes sense to an administrator on an Automatic
repository. The header's visible action is "Repair a different PR", so the page
looks like it is primarily about changing target. At 1440 the main action is
around 750px down, below two large cards. A person who arrived from "Fix with
Eli" on a failed job is asked to find the button again.

**Fix:** Put the decision and its one primary button directly under the case
summary, or in a sticky action bar below the page header: one sentence of state,
the payer and allowance in small text, and "Fix with Eli". Move the automatic
note below the decision and collapse it to one line unless it explains why an
automatic repair did not start. Make "Repair a different PR" a secondary header
action. Do not render the note at all for non-administrators.

### 3. Setup is a 504-line admin console with two repository pickers

**Pass:** Designer, First-time user

**Where:** `web/src/routes/EliRepairSetup.svelte:29-39`,
`web/src/lib/repairs/RepairSetupPanel.svelte:238-430`,
`web/src/lib/repairs/RepositoryPicker.svelte`.

**Problem:** The setup page presents "Repository repair policies" and "Link a
GitHub account" as two open cards. Each one contains a searchable repository
list with pagination, so the page has two nearly identical "Find repositories"
controls. The policy form mixes installation, provider, enablement, mode,
workflow edits and a daily budget, then ends with a disabled "Save for 0
repositories" button. A first-time administrator cannot tell whether to link an
account first, choose a repository first, or set a budget first, and there is no
visible per-repository list of what is already on. At 375 the policy card alone
is several screens long, and the bottom navigation floats over its middle while
scrolling.

**Fix:** Split setup into three explicit steps or tabs:

1. **Repairs on**: choose one repository, choose the model provider, choose
   "Review first" or "Automatic", then save.
2. **People**: link a GitHub account to a Zoomies user. Reuse one repository
   selection for verification; do not show a second full picker beside it.
3. **Advanced**: daily tries and workflow-file edits, collapsed until opened.

Existing policies should be a short table of repositories, each row showing
mode, provider, tries used and an Edit action. Replace the bulk form with
per-repository editing. Start the page with a single onboarding card when no
repository is configured.

### 4. The feature asks users to learn a private vocabulary

**Pass:** Designer, First-time user

**Where:** `web/src/routes/EliRepairs.svelte:298-303`,
`web/src/lib/repairs/DecisionCard.svelte:93-106,148-188`,
`web/src/lib/repairs/ReadinessChecklist.svelte:82-119`,
`web/src/lib/repairs/RepairSetupPanel.svelte:270-338`,
`web/src/routes/EliRepairAttempt.svelte:236-267`.

**Problem:** Before a person can start one repair, the UI introduces "offers",
"readiness", "checks", "attempts", "policies", "automatic", "Ask me first",
"providers", "allowance", "workflow edits", "PR repairs" and "fix". The same
object is called a repair, a fix, an attempt and an offer in different places.
"Investigating" in the header becomes "Preparing the fix" in the progress card
and "In progress" in the section heading. This is an internal model leaking into
the product. It is the single largest source of hesitation in the first-time
walkthrough.

**Fix:** Pick two words and use them everywhere: "repair" as the noun, "fix" as
the verb. Suggested replacements:

* "Needs you" becomes "Needs your review".
* "Review repair" becomes "Review fix".
* "Readiness" and "Can Eli act on this?" become "Can Eli fix this?".
* "Repair setup" and "Repository repair policies" become "Repair settings".
* "Attempts left" becomes "Tries left"; "attempt" survives only in technical
  details.
* "Provider" becomes "model provider" on first use, then "provider".
* "Ask me first" stays, because it is a good promise, but is explained once.

Do not use "offer" in a heading. A failed pull request is a failed pull request.

### 5. On a phone, fixed controls sit over the primary action

**Pass:** Designer, First-time user

**Where:** `web/src/lib/assistant/EliWidget.svelte:349-363` and the phone rule at
`:560-562`, `web/src/lib/shell/Nav.svelte:390-403`,
`web/src/routes/EliRepairNew.svelte:402-419`, `web/src/lib/repairs/NextSteps.svelte:43-51`,
`web/src/routes/EliRepairAttempt.svelte:325-345`.

**Problem:** At 375 by 812, the floating Eli launcher overlaps the right edge of
full-width actions such as "Fix with Eli" and "Try another attempt". The fixed
bottom navigation also sits over page content while scrolling. The current phone
tests check horizontal overflow and control size, which is necessary but not
sufficient; they do not detect one fixed control covering another.

**Fix:** On `/repairs` routes, hide the floating launcher while a primary action
is in the viewport, or move it above a reserved action bar. Reserve bottom space
for both the navigation and the launcher on these pages. Add Playwright
assertions that no button or link intersects the launcher rectangle or the
bottom navigation rectangle at 375 and 360, including after opening every
disclosure.

### 6. There are two setup paths, and they disagree

**Pass:** First-time user

**Where:** `web/src/routes/EliRepairNew.svelte:302-307,420-427`,
`web/src/lib/repairs/RepairSetup.svelte:114-172`,
`web/src/routes/EliRepairSetup.svelte:29-39`,
`web/src/lib/repairs/RepairSetupPanel.svelte:238-430`.

**Problem:** The failure page has an inline "Set up Eli for repo" card that
saves a policy with fixed defaults (manual mode, five tries, workflow edits
off), links an identity and offers a preview opt-in. The setup page has a
different form with all modes, a bulk repository picker, a provider selector and
a budget. Both are called setup. A person taught by one is surprised by the
other, and an administrator cannot tell which defaults will win.

**Fix:** Make the inline form a focused three-field wizard that only collects
what is missing for this failure, ends with "Back to this repair", and links to
"More options in Repair settings" for everything else. Do not save a policy
from the failure page when the person cannot see its mode and provider. Use the
same labels and defaults in both places.

## High impact

### 7. The decision card and the checker panel tell the same story twice

**Pass:** Designer

**Where:** `web/src/lib/repairs/DecisionCard.svelte`,
`web/src/lib/repairs/ReadinessChecklist.svelte:82-119`,
`web/src/routes/EliRepairNew.svelte:439-449`.

**Problem:** When blocked, the decision card names the blocker and shows the
button that helps. Immediately below, "Can Eli act on this?" lists the same
blocker with the same explanation and often the same button. When ready, the
decision card says "Ready to fix" and the panel says "All 9 checks passed". The
second copy adds no decision value and makes the page feel like a diagnostic
report rather than a task.

**Fix:** The decision card owns the one blocking sentence and the action. The
checker panel appears only behind "Why can't Eli fix this?" and only lists what
is blocking or unchecked. When everything passes, do not render the panel at
all; the decision card already says ready. Keep the full nine checks for
support, one press away.

### 8. The attempt page repeats the same facts in four places

**Pass:** Designer

**Where:** `web/src/routes/EliRepairAttempt.svelte:248-267` (header meta),
`web/src/lib/repairs/ProgressTrack.svelte:37-62`,
`web/src/lib/repairs/AttemptFacts.svelte:38-95`,
`web/src/lib/repairs/AttemptTimeline.svelte`.

**Problem:** The header shows status, requester, provider and start time. The
progress card shows stage, elapsed and five steps. "This attempt" shows attempt
id, status, stage, start time, duration, provider, model, source size, model
calls and branch. The timeline shows the same start and end times again. A
reader has to work out which of four status lines is current.

**Fix:** Keep the header to title, one state label and the two GitHub links. Use
one meta line for "Started 2m ago, requested by @dana". Move "This attempt",
model usage and timeline into the existing "Details" disclosure. The progress
card keeps only the current step, elapsed time and the promise that the person
may leave.

### 9. The five-step track is process theatre, especially on a phone

**Pass:** Designer

**Where:** `web/src/lib/repairs/ProgressTrack.svelte:71-174`,
`web/src/routes/EliRepairAttempt.svelte:297-302`.

**Problem:** The track renders five steps across on desktop and down the whole
first screen on mobile. Each step also repeats "Done", "In progress" or "Not
started" under its label. The useful facts are the current step and how long it
has taken. Because every repair is a black box while it runs, the track creates
the impression of more observability than the controller can actually provide.

**Fix:** Replace the five-step graphic with one line: "Preparing the fix, 1m 3s,
you can leave this page". Add a thin progress bar only if the stages are truly
ordered. Let "Show the steps" reveal the track for people who want it. Keep the
same stage words in the history row and the attempt page.

### 10. Automatic repairs competes with a person's work on the hub

**Pass:** Designer

**Where:** `web/src/routes/EliRepairs.svelte:395-411`,
`web/src/lib/repairs/AutomaticRepairs.svelte`.

**Problem:** On an administrator's hub, the Automatic repairs panel is a large
four-column table of repositories, provider names, allowance meters, latest
repairs, running counts and stop buttons. It sits between "In progress" and
"History". It is an operations console, not a personal inbox. Its problems are
already surfaced elsewhere, and its stop actions are destructive enough that
they should not be one accidental click from a failed offer.

**Fix:** Move Automatic repairs to Repair settings, or to a separate
"Automation" tab for administrators. On the hub, show one line only when
something is wrong: "2 repositories repair automatically, 1 cannot start. Open
automation." Keep Stop automatic repairs in the settings view where the current
mode is visible.

### 11. History is a power tool sitting on the main hub

**Pass:** Designer

**Where:** `web/src/lib/repairs/RepairHistory.svelte:379-543`,
`web/src/routes/EliRepairs.svelte:413-415`.

**Problem:** History includes filters, a repository text field, a trigger
selector, select mode, select-all-matching, bulk clear, pagination and page-size
preferences. That is a records-management screen. On the hub it competes with
the two things a person actually arrived to do, and on an empty fleet it opens
by itself and becomes the page.

**Fix:** Give History its own route (`/repairs/history`) or a tab that is not
open by default. On the hub show the last three repairs as plain rows with
"Open history". Keep filters, selection and clearing on the history page only.

### 12. "Repair a PR" asks for a URL instead of a recent failure

**Pass:** First-time user

**Where:** `web/src/lib/repairs/RepairTargetDialog.svelte:23-64`,
`web/src/routes/EliRepairs.svelte:302`.

**Problem:** The hub's primary button opens a dialog asking the person to paste a
GitHub pull request URL. Most people arrive from a failed job or a failed pull
request they can see in Zoomies; they do not have the URL to hand. This is the
slowest possible way to start, presented as the only way.

**Fix:** Make the dialog a picker of recent failed pull requests and failed jobs,
searchable by repository or title, with a "Paste a link" fallback. Reuse the
existing repository and job list endpoints. Selecting a row should open the
repair review page, not start a repair.

### 13. Offers require two pages before a fix can start

**Pass:** First-time user

**Where:** `web/src/lib/repairs/OfferList.svelte:80-97`,
`web/src/routes/EliRepairs.svelte:360-370`,
`web/src/routes/EliRepairNew.svelte`.

**Problem:** An offer says "Review repair" and opens the repair page; only there
does the person see the payer, the allowance and "Fix with Eli". Two page loads
and two decisions for one action. The first page is already showing the failed
jobs, so the second page's case summary adds little.

**Fix:** Let an offer open in a drawer or expand in place with the case summary,
payer, tries left and "Fix with Eli". Keep the full repair page for editing the
instruction and for reading every check. If the drawer is not feasible, make
"Review repair" the primary press and put "Fix with Eli" in the sticky header on
the repair page.

### 14. "What to try next" is generic and does not use the failure evidence

**Pass:** First-time user

**Where:** `web/src/lib/repairs/NextSteps.svelte:37-52`,
`web/src/lib/repairs/guidance.ts`.

**Problem:** A failed repair with `unsupported_fix` shows two numbered pieces of
advice: "Add a note that points at the file or the test, then try again" and
"Read the error output: the quickest fix may be yours." That is true of every
failed repair, so it reads as filler. The page knows the failure code, the
failed job, the step and the log; the next step should use one of them.

**Fix:** Render one recommendation derived from the failure code, plus one
evidence link. If the failure is a fleet fault, say so and link the host. If the
failure is a workflow failure, link the failed job and its log. Only then offer
"Try another repair"; remove the numbered generic list.

### 15. The same state has three different names

**Pass:** Designer

**Where:** `web/src/lib/status.ts:882-930`,
`web/src/lib/repairs/journey.ts`,
`web/src/lib/repairs/history.ts`,
`web/src/routes/EliRepairAttempt.svelte:253-260`.

**Problem:** A running repair is "Investigating" in the page header, "Preparing
the fix" in the progress track, and "In progress" in the hub section. A failed
repair is "Could not repair" in one place and "Checks failed" in another. A
person cannot tell whether these are separate states or the same one described
differently.

**Fix:** One state label map used by every surface, with the stage as a
secondary sentence. Keep the four user-facing states: "Waiting", "Working",
"Checks failed", "Checks passed". Show the stage only on the attempt page and in
history details.

## Nice to have

### 16. The empty state is written for the system, not the person

**Pass:** First-time user

**Where:** `web/src/routes/EliRepairs.svelte:371-379`.

**Problem:** "Offers appear when a pull request's checks fail on a repository set
to Ask me first" explains the implementation before the benefit. "Repair a pull
request" is a second name for the header's "Repair a PR".

**Fix:** "No failed pull requests need you. When checks fail on a repository Eli
may fix, they appear here." One button: "Fix a failed check".

### 17. Counts are repeated in the navigation, headings and rows

**Pass:** Designer

**Where:** `web/src/routes/EliRepairs.svelte:146-154`,
`web/src/lib/repairs/OfferList.svelte:111`,
`web/src/lib/repairs/ActiveRepairs.svelte:81`.

**Problem:** "Needs you 1" in the navigation, "1 offer" in the section summary,
"1 offer needs review" in the rail and "1 offer" in the panel header. The number
is useful once.

**Fix:** Keep the count in the section navigation and the section summary; remove
it from the panel action badge on the hub. On the full-page panel, keep the count
because the navigation is not visible.

### 18. "All 9 checks passed" is an anti-climax

**Pass:** Designer

**Where:** `web/src/lib/repairs/ReadinessChecklist.svelte:99-107`.

**Problem:** The last card on the repair page is a collapsed disclosure that says
everything passed. It repeats the decision card and ends the page on
maintenance rather than the action.

**Fix:** Remove the card when everything passes. Put one line under the decision
card: "All nine checks passed. Checking again never uses a try." Keep the panel
only when something is blocked.

### 19. Technical identifiers are visible in the everyday path

**Pass:** Designer

**Where:** `web/src/routes/EliRepairAttempt.svelte:236-245,325-346`,
`web/src/lib/repairs/AttemptFacts.svelte:41-45`.

**Problem:** The breadcrumb says "Attempt rep-run"; the header and facts show the
attempt id with a copy button; the header shows provider and model badges. These
are support details, not the answer to "what happened to my fix".

**Fix:** Keep the id in Details and in "Copy investigation details". Show the
provider and model only in Details, and only the payer in the decision card.

### 20. The setup form offers a disabled "Save for 0 repositories"

**Pass:** First-time user

**Where:** `web/src/lib/repairs/RepairSetupPanel.svelte:327-350`.

**Problem:** The primary button is disabled with a label that says "Save for 0
repositories" until the hidden repository list is used. A person who has not
scrolled to the list sees a dead action and no reason.

**Fix:** Replace the disabled button with "Choose repositories" while the
selection is empty, and scroll to the picker when pressed. Once one is selected,
change it to "Save for 1 repository". Keep the count in the label only when it
is not zero.

### 21. The page names disagree

**Pass:** First-time user

**Where:** `web/src/routes/EliRepairs.svelte:298-302`,
`web/src/routes/EliRepairNew.svelte:365-372`,
`web/src/lib/repairs/RepairTargetDialog.svelte:42-44`.

**Problem:** The feature is "Eli repairs", the button is "Repair a PR", the page
is "Repair a failed check", the breadcrumb is "New repair", and the dialog is
"Repair a different PR". Five names for one destination.

**Fix:** Use "Repairs" for the feature, "Repair a failed check" for the page and
"Choose a pull request" for the dialog. Use the same words in the navigation,
the header and the empty state.

### 22. Phone tests prove width, not reachability

**Pass:** Designer

**Where:** `web/tests/mobile.spec.ts:1290-1430`,
`web/tests/repair-attempt.spec.ts:404-440`.

**Problem:** The tests assert no horizontal overflow and at least 16px controls.
They do not assert that the floating Eli launcher, the bottom navigation or a
dialog leaves the primary action clickable. The screenshots show the launcher
over "Fix with Eli" and "Try another attempt".

**Fix:** Add an occlusion test for these pages: for every visible button and
link in a primary action group, assert that its bounding box does not intersect
the launcher or navigation rectangles at 375 and 360. Add the same check after
opening the setup wizard, the check list and the attempt details.

## The top five, in order

1. Remove the duplicated status surfaces on the hub and make the first useful
   action visible without scrolling.
2. Put "Fix with Eli" or its blocker directly under the case, and stop showing
   an automatic-repair note above it to people who cannot act on it.
3. Rebuild setup as a three-step flow with one repository picker, moving
   budgets and workflow edits behind Advanced.
4. Replace the private vocabulary ("offer", "readiness", "attempt", "policy")
   with "failed pull request", "tries" and "repair settings".
5. Reserve mobile space for the floating Eli button and bottom navigation, then
   add a test that fails when either covers a primary action.
