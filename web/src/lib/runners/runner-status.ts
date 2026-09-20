import type { Runner } from '../api/types';
import { runnerStatus, toneTokens, type StatusTone } from '../status';

type StatusRunner = Pick<Runner, 'state' | 'cpu_resource'>;

const activity: Record<string, { label: string; title: string; detail: string; tone: StatusTone }> =
  {
    maximum_zoomies: {
      label: 'Squirrel spotted',
      title: 'Maximum zoomies',
      detail: 'A major CPU boost is active. This runner is borrowing spare host capacity.',
      tone: 'busy',
    },
    zoomies: {
      label: 'Rabbit spotted',
      title: 'Extra zoomies',
      detail: 'Extra CPU is available to this runner while its host has room to spare.',
      tone: 'busy',
    },
    throttled: {
      label: 'Leash tightened',
      title: 'Host pressure protection',
      detail: 'CPU has been reduced to protect the host. It can recover as pressure eases.',
      tone: 'draining',
    },
  };

const quietCPU: Record<string, string> = {
  observing: 'Nose to the wind: spare CPU is being observed, without changing this allocation.',
  guaranteed: 'Steady paws: this runner has its guaranteed CPU allocation.',
  sit_and_stay: 'Sit and stay: elastic CPU is off for this pool.',
};

/** Lifecycle stays authoritative. Only an active allocation replaces Busy/Idle. */
export function runnerDisplayStatus(runner: StatusRunner) {
  const lifecycle = runnerStatus(runner.state);
  const canBoost = runner.state === 'busy' || runner.state === 'idle';
  const resource = runner.cpu_resource;
  const active = canBoost ? activity[resource?.state ?? ''] : undefined;
  const key = active ? resource!.state! : lifecycle.key;
  const detail = active?.detail ?? lifecycle.hint ?? 'No runner state has been reported.';
  const cpuDetail = !active && canBoost ? quietCPU[resource?.state ?? ''] : undefined;
  return {
    key,
    label: active?.label ?? lifecycle.label,
    title: active?.title ?? lifecycle.label,
    detail,
    cpuDetail,
    lifecycle,
    active: Boolean(active),
    canBoost,
    ...toneTokens(active?.tone ?? lifecycle.tone),
  };
}
