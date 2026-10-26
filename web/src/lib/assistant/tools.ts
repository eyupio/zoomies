/**
 * What Eli's tools are called to a person. The tool names are the MCP server's,
 * which are written for an agent; a person reading "Looked at list_runners" is
 * reading somebody else's variable.
 */
const LABELS: Record<string, string> = {
  fleet_status: 'the fleet at a glance',
  list_problems: 'the problems',
  list_jobs: 'jobs',
  get_job: 'a job',
  get_retained_comparison: 'a retained historical comparison',
  get_pr_delay: 'a PR’s observed required-check delay',
  job_stats: 'job statistics',
  list_runners: 'runners',
  list_pools: 'pools',
  list_hosts: 'hosts',
  host_health: 'a host’s health',
  label_advice: 'labels',
  get_runner_log: 'a runner’s log',
  kennel_overview: 'Kennel Club',
  kennel_repository: 'a repository in Kennel Club',
  kennel_findings: 'Kennel Club findings',
  list_providers: 'infrastructure providers',
  provider_pairings: 'which providers serve which pools',
  list_machines: 'rented machines',
  get_catalog: 'the catalog of codes and checks',
  get_usage: 'usage and cost',
  recent_events: 'what changed lately',
  wait_for_change: 'the next change',
  scaling_decisions: 'the scheduler’s decisions',
  fleet_samples: 'the fleet minute by minute',
  host_samples: 'hosts minute by minute',
  runner_timeline: 'a runner’s timeline',
  list_workflow_runs: 'workflow runs',
  list_elis_repairs: 'the repairs history',
  get_eli_repair: 'a pull request’s repair',
  queue_wait_breakdown: 'why jobs waited',
  compare_periods: 'what got worse or better',
  host_pressure: 'whether a host’s failures came under pressure',
  startup_failures: 'what keeps failing to start',
  recurring_failures: 'which jobs keep failing',
};

export function toolLabel(name: string): string {
  return LABELS[name] ?? name.replace(/_/g, ' ');
}
