// Overview: what this fleet is doing right now, in the portal's look.
//
// Today's page is six tiles, a lifecycle diagram and two paragraphs, and an
// operator reads it to answer one question: is anything wrong. The mock answers
// it first, in a stage, and puts the evidence under it as four numbers, a card
// per pool and the problems with what to do about each.
//
// Every number is computed from the fixture by fleetCounts, so this page and
// the pages it links to cannot disagree.
import { button, esc } from "/review/ui.js";
import { poolState } from "./derive.js";
import { rowAction } from "./list.js";
import { icon } from "/review/icons.js";
import { FLEET, fleetCounts } from "/signed-in/fixture.js";
import {
  card,
  checklist,
  clip,
  tile,
  failure,
  meter,
  pill,
  relative,
  shell,
  skeleton,
  stage,
  stat,
  staleBanner,
  steps,
} from "/signed-in/kit.js";

const STATES = [
  "attention",
  "built",
  "healthy",
  "first-run",
  "loading",
  "error",
  "stale",
  "collapsed",
];
const LABELS = {
  attention: "Needs attention",
  built: "Built on the kit, with the band",
  healthy: "Healthy",
  "first-run": "First run, nothing connected",
  loading: "Loading",
  error: "First fetch failed",
  stale: "Showing stale data",
  collapsed: "Collapsed sidebar",
};

/** The fleet with everything that is wrong taken out, derived from the same rows. */
function healthy(fleet) {
  const okHosts = fleet.hosts.filter((h) => h.healthy && !h.cordoned);
  const okInstallations = new Set(
    fleet.installations.filter((i) => i.healthy).map((i) => i.id),
  );
  const hostIds = new Set(okHosts.map((h) => h.id));
  const pools = fleet.pools.filter(
    (p) =>
      okInstallations.has(p.installation_id) &&
      fleet.runners.some((r) => r.pool_id === p.id && hostIds.has(r.host_id)),
  );
  const poolIds = new Set(pools.map((p) => p.id));
  return {
    ...fleet,
    pools,
    hosts: okHosts,
    installations: fleet.installations.filter((i) => i.healthy),
    runners: fleet.runners.filter(
      (r) =>
        poolIds.has(r.pool_id) &&
        hostIds.has(r.host_id) &&
        r.state !== "failed",
    ),
    jobs: fleet.jobs.filter((j) => poolIds.has(j.pool_id)),
    problems: [],
  };
}

function poolCard(pool, fleet) {
  const own = fleetCounts(fleet).pools[pool.id];
  const { state, label } = poolState(pool, fleet);
  return card({
    icon: "boxes",
    title: pool.name,
    href: "#screen=pools",
    actions: pill({ state, label }),
    html:
      `<dl class="k-facts"><div><dt>Runners</dt><dd>${own.runners} of ${pool.max_runners}</dd></div>` +
      `<div><dt>Waiting</dt><dd>${own.queued}</dd></div></dl>` +
      meter({
        value: pool.utilisation,
        label: `${Math.round(pool.utilisation * 100)}% busy`,
      }),
  });
}

function problemRow(problem) {
  return (
    `<li>${pill({ state: problem.severity === "error" ? "danger" : "pending", label: problem.severity === "error" ? "Error" : "Warning" })}` +
    `<div class="k-list-main"><strong>${esc(problem.title)}</strong><span>${esc(problem.fix)}</span></div>` +
    `${rowAction("Open", problem.title)}</li>`
  );
}

function recentRow(job) {
  const ok = job.conclusion === "success";
  return (
    `<li>${pill({ state: ok ? "idle" : "danger", label: ok ? "Passed" : "Failed" })}` +
    `<div class="k-list-main"><strong>${esc(job.repo)}</strong><span>${esc(job.workflow)} / ${esc(job.job_name)}</span></div>` +
    `<span class="k-meter-value">${esc(relative(job.completed_at))}</span></li>`
  );
}

function populated(fleet, state) {
  const counts = fleetCounts(fleet);
  const problems = fleet.problems;
  const oldest = fleet.jobs
    .filter((j) => j.state === "queued")
    .sort((a, b) => a.queued_at.localeCompare(b.queued_at))[0];
  const calm = problems.length === 0;
  const body =
    stage({
      eyebrow: "Fleet status",
      headline: calm
        ? "Everything is running."
        : `${problems.length} ${problems.length === 1 ? "thing needs" : "things need"} you.`,
      accent: `${counts.jobs.running} ${counts.jobs.running === 1 ? "job is" : "jobs are"} running.`,
      lede: calm
        ? `${counts.hosts.used} of ${counts.hosts.slots} host slots are in use and nothing is waiting longer than it should.`
        : problems[0].title + ". " + problems[0].fix,
      html: calm
        ? ""
        : button({
            label: "Review the problems",
            variant: "primary",
            href: "#",
          }),
    }) +
    `<div class="k-grid">` +
    stat({
      label: "Runners",
      value: counts.runners.live,
      note: `${counts.runners.busy} busy, ${counts.runners.idle} idle`,
      href: "#screen=runners",
      icon: "server",
    }) +
    stat({
      label: "Jobs running",
      value: counts.jobs.running,
      note: "across all pools",
      href: "#screen=jobs",
      icon: "workflow",
    }) +
    stat({
      label: "Waiting",
      value: counts.jobs.queued,
      state: counts.jobs.queued ? "pending" : "idle",
      stateLabel: counts.jobs.queued ? "Queued" : "Clear",
      note: oldest ? `oldest ${relative(oldest.queued_at)}` : "nothing queued",
      href: "#screen=queue",
      icon: "list-ordered",
    }) +
    stat({
      label: "Host slots",
      value: `${counts.hosts.used}/${counts.hosts.slots}`,
      state: counts.hosts.unhealthy ? "danger" : "idle",
      stateLabel: counts.hosts.unhealthy
        ? `${counts.hosts.unhealthy} host down`
        : "All hosts up",
      href: "#screen=hosts",
      icon: "hard-drive",
    }) +
    `</div><h2 class="k-section-title">Pools</h2><div class="k-grid wide">${fleet.pools.map((p) => poolCard(p, fleet)).join("")}</div>` +
    (calm
      ? card({
          icon: "clock",
          title: "Recently finished",
          html: `<ul class="k-list">${fleet.jobs
            .filter((j) => j.state === "completed")
            .map(recentRow)
            .join("")}</ul>`,
        })
      : card({
          icon: "triangle-alert",
          title: "Needs attention",
          tone: "danger",
          html: `<ul class="k-list">${problems.map(problemRow).join("")}</ul>`,
        }));
  return shell({
    page: "overview",
    title: "Overview",
    eyebrow: "Fleet",
    subtitle: "What this fleet is doing right now.",
    actions: button({
      label: "New pool",
      variant: "primary",
      icon: "plus",
      href: "#screen=pools",
    }),
    collapsed: state === "collapsed",
    banner: state === "stale" ? staleBanner({ ago: "6 minutes ago" }) : "",
    body,
  });
}

/**
 * The Overview as the second pull request builds it: the band the product has
 * (its sentence, its four figures along the lower edge and the week beside the
 * words), and under it the mock's cards. The four figures are not repeated as
 * cards, because the band carries them; Needs attention is the worst three with
 * the rest behind the drawer, so what needs a person never pushes the fleet
 * below the fold; the pools keep their ceiling tick and badge on the card; and
 * the panels the first mock left out (the failures, the running jobs, the
 * events, History and Diagnostics) keep their place in the same grammar.
 */
function punchCard() {
  const days = ["Wed", "Thu", "Fri", "Sat", "Sun", "Mon", "Tue"];
  let seed = 7;
  const level = (d, h) => {
    seed = (seed * 9301 + 49297) % 233280;
    if (d >= 3 && d <= 4) return seed % 5 === 0 ? 1 : 0;
    if (h < 7 || h > 19) return seed % 4 === 0 ? 1 : 0;
    return 1 + ((seed + h) % 4);
  };
  const rows = days
    .map((day, d) => {
      const cells = Array.from({ length: 24 }, (_, h) => {
        const today = d === 6;
        const future = today && h > 10;
        const current = today && h === 10;
        const l = future ? 0 : level(d, h);
        const failing = !future && (d * 24 + h) % 29 === 3 && l > 0;
        return `<i class="k-sq${current ? " is-current" : ""}" data-level="${l}"${failing ? " data-failing" : ""}${future ? " data-future" : ""}></i>`;
      }).join("");
      return `<span class="k-day">${day}</span>${cells}`;
    })
    .join("");
  return (
    `<div class="k-punch" aria-label="The week, an hour a square"><div class="k-punch-head"><span class="k-punch-title">This week</span><span class="k-punch-summary">1,412 jobs, 31 failed</span></div>` +
    `<div class="k-punch-grid">${rows}</div><div class="k-punch-axis"><span>00</span><span>06</span><span>12</span><span>18</span></div></div>`
  );
}

function figure({ label, value, note, href }) {
  return `<a class="k-figure" href="${esc(href)}"><span class="k-figure-label">${esc(label)}</span><span class="k-figure-value">${esc(value)}</span>${note ? `<span class="k-figure-note">${esc(note)}</span>` : ""}</a>`;
}

function builtStage(fleet) {
  const counts = fleetCounts(fleet);
  const problems = fleet.problems;
  return (
    `<section class="k-stage has-aside"><div class="k-stage-words">` +
    `<p class="k-stage-eyebrow">Fleet status</p>` +
    `<p class="k-stage-headline">${problems.length} things need you.<br><span class="k-accent">${counts.jobs.running} jobs are running.</span></p>` +
    `<div class="k-rule" aria-hidden="true"></div>` +
    `<p class="k-stage-lede">${problems.filter((p) => p.severity === "error").length} errors and ${problems.filter((p) => p.severity === "warning").length} warning need your attention.</p>` +
    button({ label: "Review", variant: "primary", href: "#" }) +
    `</div><div class="k-stage-aside">${punchCard()}</div>` +
    `<div class="k-stage-figures">` +
    figure({
      label: "Queued jobs",
      value: counts.jobs.queued,
      href: "#screen=queue",
    }) +
    figure({
      label: "Running jobs",
      value: counts.jobs.running,
      href: "#screen=jobs",
    }) +
    figure({
      label: "Live runners",
      value: counts.runners.live,
      note: `${counts.hosts.used} of ${counts.hosts.slots} slots on ${fleet.hosts.filter((h) => h.healthy).length} connected hosts`,
      href: "#screen=runners",
    }) +
    figure({
      label: "Median queue wait",
      value: "19s",
      note: "p95 26s",
      href: "#screen=queue",
    }) +
    `</div></section><p class="k-coverage">Waits and outcomes cover the last hour.</p>`
  );
}

function builtPoolCard(pool, fleet) {
  const own = fleetCounts(fleet).pools[pool.id];
  const ceiling = own.runners >= pool.max_runners && own.queued > 0;
  const { state, label } = ceiling
    ? { state: "pending", label: "At its ceiling" }
    : poolState(pool, fleet);
  return card({
    icon: "boxes",
    title: pool.name,
    href: "#screen=pools",
    actions: pill({ state, label }),
    tone: ceiling ? "pending" : "neutral",
    html:
      `<dl class="k-facts"><div><dt>Runners</dt><dd>${own.runners} of ${pool.max_runners}</dd></div>` +
      `<div><dt>Waiting</dt><dd>${own.queued}${ceiling ? " and nowhere to run" : ""}</dd></div></dl>` +
      `<span class="k-meter k-meter-ceiling"><span class="k-meter-track" aria-hidden="true"><span class="k-meter-fill" style="--k-fill: ${Math.round(pool.utilisation * 100)}%"></span><i class="k-meter-tick" style="--k-tick: ${Math.round((own.runners / pool.max_runners) * 100)}%"></i></span><span class="k-meter-value">${Math.round(pool.utilisation * 100)}% busy</span></span>`,
  });
}

function activeJobRow(job) {
  return (
    `<li><div class="k-list-main"><strong>${clip(job.repo)}</strong><span>${clip(`${job.workflow} / ${job.job_name}`)}</span></div>` +
    `<span class="k-mono">${esc(job.runner_name ?? "")}</span><span class="k-meter-value">${esc(relative(job.started_at))}</span></li>`
  );
}

function eventRow({ state, label, text, who, when }) {
  return `<li>${pill({ state, label })}<div class="k-list-main"><strong>${esc(text)}</strong><span>${esc(who)}</span></div><span class="k-meter-value">${esc(when)}</span></li>`;
}

function fold(title, summary, icon) {
  return `<details class="k-card k-fold"><summary><span class="k-card-head">${tile({ icon })}<span class="k-card-heading"><span class="k-card-title">${esc(title)}</span><span class="k-fold-summary">${esc(summary)}</span></span>${iconRight()}</span></summary></details>`;
}
const iconRight = () =>
  `<span class="k-fold-chevron" aria-hidden="true">${icon("chevron-right", 16)}</span>`;

function built(fleet) {
  const counts = fleetCounts(fleet);
  const problems = fleet.problems;
  const worst = [...problems].sort(
    (a, b) =>
      (a.severity === "error" ? 0 : 1) - (b.severity === "error" ? 0 : 1),
  );
  const shown = worst.slice(0, 3);
  const running = fleet.jobs.filter((j) => j.state === "in_progress");
  const body =
    builtStage(fleet) +
    card({
      icon: "triangle-alert",
      title: "Needs attention",
      tone: "danger",
      actions: `<a class="k-card-link k-more" href="#">${problems.length > shown.length ? `and ${problems.length - shown.length} more, in the drawer` : "Open the drawer"}</a>`,
      html: `<ul class="k-list">${shown.map(problemRow).join("")}</ul>`,
    }) +
    `<h2 class="k-section-title">Pools <span class="k-section-note">${fleet.pools.length} enabled</span></h2><div class="k-grid wide">${fleet.pools.map((p) => builtPoolCard(p, fleet)).join("")}</div>` +
    `<section class="k-card k-line" data-tone="danger">${tile({ icon: "zap" })}<strong>2 failures need review</strong><span>Newest: acme-corp/api, test (linux), 6 min ago.</span><a class="k-card-link" href="#screen=jobs">Review failures</a></section>` +
    `<div class="k-work">` +
    card({
      icon: "workflow",
      title: "Active jobs",
      actions:
        pill({ state: "busy", label: `${counts.jobs.running} running` }) +
        `<a class="k-card-link" href="#screen=jobs">View all</a>`,
      html: `<ul class="k-list k-list-jobs">${running.map(activeJobRow).join("")}</ul>`,
    }) +
    card({
      icon: "activity",
      title: "Recent events",
      actions: pill({ state: "danger", label: "1 failed, 1 ours" }),
      html:
        `<p class="k-card-note">Newest first. 11 of 13 kinds.</p><ul class="k-list">` +
        eventRow({
          state: "danger",
          label: "Failed",
          text: "A runner failed",
          who: "pool_build-003 · pi-rack-03",
          when: "6 min ago",
        }) +
        eventRow({
          state: "busy",
          label: "Scaled",
          text: "Scaled up 4 to 5",
          who: "linux-build",
          when: "8 min ago",
        }) +
        eventRow({
          state: "pending",
          label: "Stopped",
          text: "A job's runner stopped under it",
          who: "acme-corp/web · CI / package",
          when: "20 min ago",
        }) +
        eventRow({
          state: "idle",
          label: "Ready",
          text: "A runner came up ready",
          who: "linux-gpu-002 · gpu-box-1",
          when: "24 min ago",
        }) +
        eventRow({
          state: "idle",
          label: "Passed",
          text: "A job passed",
          who: "acme-corp/api · CI / test",
          when: "31 min ago",
        }) +
        `</ul>`,
    }) +
    `</div>` +
    fold(
      "History",
      "The last 7 days: 1,412 jobs, 31 failed, busiest on Tuesday.",
      "clock",
    ) +
    `<div class="k-grid wide">` +
    fold(
      "Host capacity",
      "9 of 22 slots on 3 connected hosts; pi-rack-03 not reporting.",
      "hard-drive",
    ) +
    fold(
      "Startup and registration",
      "Median 41s to ready, p95 1m 12s.",
      "gauge",
    ) +
    `</div>`;
  return shell({
    page: "overview",
    title: "Overview",
    eyebrow: "Fleet",
    subtitle: "What the fleet is doing, and what needs you.",
    body,
  });
}

function firstRun() {
  return shell({
    page: "overview",
    title: "Overview",
    eyebrow: "Fleet",
    subtitle: "What this fleet is doing right now.",
    body:
      stage({
        eyebrow: "Welcome to Zoomies",
        headline: "Your fleet is empty.",
        accent: "Three steps fill it.",
        lede: "Connect GitHub, give Zoomies a machine to run on, and say which jobs it should pick up.",
      }) +
      `<div class="k-grid wide">` +
      checklist({
        items: [
          { label: "Create the first account", done: true },
          {
            label: "Connect a GitHub App",
            text: "So Zoomies can see queued jobs and register runners.",
            action: "Connect GitHub",
          },
          { label: "Add a host", text: "The machine that will run your jobs." },
          {
            label: "Create a pool",
            text: "Which labels it answers to, and how many runners it may start.",
          },
        ],
      }) +
      card({
        icon: "compass",
        title: "How it works",
        html: steps([
          {
            title: "A job is queued",
            text: "GitHub tells Zoomies a job is waiting for a label it answers to.",
          },
          {
            title: "A runner starts",
            text: "One container, on a host with room, for that job alone.",
          },
          {
            title: "It is destroyed",
            text: "When the job finishes, so the next job never sees this one.",
          },
        ]),
      }) +
      `</div>`,
  });
}

function loading() {
  return shell({
    page: "overview",
    title: "Overview",
    eyebrow: "Fleet",
    subtitle: "What this fleet is doing right now.",
    body: card({ html: skeleton({ rows: 6, label: "Loading the fleet" }) }),
  });
}

function broken() {
  return shell({
    page: "overview",
    title: "Overview",
    eyebrow: "Fleet",
    subtitle: "What this fleet is doing right now.",
    body: card({
      html: failure({
        title: "The controller did not answer",
        text: "The Overview could not be loaded. Nothing has changed on your fleet; this page just cannot see it. Check that the controller is running, then try again.",
      }),
    }),
  });
}

export default {
  id: "overview",
  title: "Overview",
  states: STATES,
  labels: LABELS,
  currentAlt:
    "Today’s Overview: six tiles of counts, a diagram of the runner lifecycle and two paragraphs, on one long page.",
  render(_concept, state) {
    if (state === "built") return built(FLEET);
    if (state === "first-run") return firstRun();
    if (state === "loading") return loading();
    if (state === "error") return broken();
    const fleet = state === "healthy" ? healthy(FLEET) : FLEET;
    return populated(fleet, state);
  },
};
