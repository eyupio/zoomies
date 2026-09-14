/**
 * The provider wizard's draft, and the words it renders with.
 *
 * Pure, and in a `.ts` rather than in the form's `<script module>`, for one
 * reason: the unit tests run under Node with no Svelte compiler, so machinery
 * kept inside a component is machinery nothing can test without a browser.
 * `PoolWizardForm.svelte` keeps its draft in a module block for the same
 * purpose; this is the same idea one file further out.
 *
 * Numbers are held as strings because that is what a text input gives back,
 * and because '' and '0' are different answers -- one is "not filled in", the
 * other is a deliberate zero, and `max_machines: 0` is the setting that rents
 * nothing. They become numbers exactly once, in `toProviderBody`.
 */
import type {
  BackendKind,
  Provider,
  ProviderInput,
  ProviderKindName,
  ProviderSetting,
} from '../api/types';

export interface WizardStepDef {
  id: string;
  title: string;
  description?: string;
}

/**
 * The steps, in the order the questions are asked: where it is and how we
 * sign in, what it should build, how big a machine is, what it may spend, and
 * then what the controller makes of all that.
 */
export const WIZARD_STEPS: readonly WizardStepDef[] = [
  {
    id: 'connect',
    title: 'Connect',
    description: 'Where this controller reaches the provider, and what it signs in with.',
  },
  {
    id: 'placement',
    title: 'Placement',
    description: "The driver's own settings: where a machine is built and from what.",
  },
  {
    id: 'machine',
    title: 'Machine',
    description: 'The one machine shape this provider offers. A second shape is a second provider.',
  },
  {
    id: 'limits',
    title: 'Limits',
    description: 'The ceilings. Nothing is rented until the first of these is above zero.',
  },
  { id: 'review', title: 'Review', description: 'What the controller makes of it.' },
];

/** Which API field names live on which step, so a rejection lands on the right one. */
export const STEP_FIELDS: readonly (readonly string[])[] = [
  ['kind', 'name', 'endpoint', 'credential', 'ca_pem', 'insecure_skip_verify'],
  ['settings'],
  [
    'machine_capacity',
    'machine_backend',
    'machine_cpus',
    'machine_memory_mb',
    'machine_disk_mb',
    'machine_platform',
    'machine_labels',
  ],
  ['max_machines', 'max_creates_in_flight', 'idle_timeout', 'cost_per_machine_hour', 'enabled'],
  [],
];

/** Human labels for the field names the API rejects things under. */
export const FIELD_LABELS: Readonly<Record<string, string>> = {
  kind: 'Kind',
  name: 'Name',
  endpoint: 'Address',
  credential: 'Credential',
  ca_pem: 'Certificate authority',
  insecure_skip_verify: 'Certificate verification',
  settings: 'Placement settings',
  machine_capacity: 'Runner slots per machine',
  machine_backend: 'Backend',
  machine_cpus: 'vCPUs',
  machine_memory_mb: 'Memory',
  machine_disk_mb: 'Disk',
  machine_platform: 'Operating system',
  machine_labels: 'Machine labels',
  max_machines: 'Maximum machines',
  max_creates_in_flight: 'Machines built at once',
  idle_timeout: 'Idle timeout',
  cost_per_machine_hour: 'Cost per machine-hour',
  enabled: 'Enabled',
};

/**
 * Which step a field belongs to. A driver's own setting arrives as
 * `settings.<key>`, which is the placement step whatever the key is; anything
 * unrecognised falls back to the review step, where every message is shown at
 * once.
 */
export function stepForField(field: string): number {
  const key = field.startsWith('settings.') ? 'settings' : field;
  const index = STEP_FIELDS.findIndex((fields) => fields.includes(key));
  return index === -1 ? WIZARD_STEPS.length - 1 : index;
}

export interface ProviderDraft {
  /**
   * Plain strings, not the generated unions. Every control in the form binds a
   * string, and a draft typed as the union would make the two disagree the
   * moment a `<select>` was involved; the narrowing happens once, on the way
   * out, in `toProviderBody`.
   */
  kind: string;
  name: string;
  endpoint: string;
  ca_pem: string;
  insecure_skip_verify: boolean;
  /**
   * Never populated from a provider that already exists: the controller seals
   * it and never gives it back, so an empty box on an edit means "leave the
   * stored one alone" rather than "erase it".
   */
  credential: string;
  /** The driver's own answers, keyed by SettingSpec.key. */
  settings: Record<string, string>;
  machine_labels: Record<string, string>;
  machine_capacity: string;
  machine_backend: string;
  machine_cpus: string;
  machine_memory_mb: string;
  machine_disk_mb: string;
  machine_os: string;
  machine_os_version: string;
  machine_arch: string;
  max_machines: string;
  max_creates_in_flight: string;
  idle_timeout: string;
  cost_per_machine_hour: string;
  enabled: boolean;
}

export function emptyDraft(): ProviderDraft {
  return {
    kind: '',
    name: '',
    endpoint: '',
    ca_pem: '',
    insecure_skip_verify: false,
    credential: '',
    settings: {},
    machine_labels: {},
    machine_capacity: '2',
    machine_backend: 'docker',
    machine_cpus: '',
    machine_memory_mb: '',
    machine_disk_mb: '',
    machine_os: '',
    machine_os_version: '',
    machine_arch: '',
    // Zero rents nothing, which is the safe default and the one an operator
    // has to change on purpose before any money is spent.
    max_machines: '0',
    max_creates_in_flight: '1',
    idle_timeout: '15m',
    cost_per_machine_hour: '',
    enabled: true,
  };
}

function fromNumber(value: number | undefined): string {
  return value === undefined || value === null ? '' : String(value);
}

export function draftFromProvider(provider: Provider): ProviderDraft {
  const base = emptyDraft();
  return {
    ...base,
    kind: provider.kind ?? '',
    name: provider.name ?? '',
    endpoint: provider.endpoint ?? '',
    insecure_skip_verify: provider.insecure_skip_verify === true,
    settings: { ...(provider.settings ?? {}) },
    machine_labels: { ...(provider.machine_labels ?? {}) },
    machine_capacity: fromNumber(provider.machine_capacity) || base.machine_capacity,
    machine_backend: provider.machine_backend ?? base.machine_backend,
    machine_cpus: fromNumber(provider.machine_cpus),
    machine_memory_mb: fromNumber(provider.machine_memory_mb),
    machine_disk_mb: fromNumber(provider.machine_disk_mb),
    machine_os: provider.machine_platform?.os ?? '',
    machine_os_version: provider.machine_platform?.os_version ?? '',
    machine_arch: provider.machine_platform?.arch ?? '',
    max_machines: fromNumber(provider.max_machines) || '0',
    max_creates_in_flight: fromNumber(provider.max_creates_in_flight) || '1',
    idle_timeout: msToDuration(provider.idle_timeout_ms) || base.idle_timeout,
    cost_per_machine_hour: fromNumber(provider.cost_per_machine_hour),
    enabled: provider.enabled !== false,
  };
}

/** A Go duration from milliseconds, in the units an operator typed them in. */
function msToDuration(ms: number | undefined): string {
  if (!ms || ms <= 0) return '';
  if (ms % 3_600_000 === 0) return `${ms / 3_600_000}h`;
  if (ms % 60_000 === 0) return `${ms / 60_000}m`;
  return `${Math.round(ms / 1000)}s`;
}

/**
 * Fill in the answers the driver has a default for, without touching one the
 * operator has already given.
 *
 * A form that comes filled in is the difference between a wizard and an
 * interrogation -- `AddHostFlow` arrives with the controller address already
 * in it for the same reason -- and a default the driver published is a better
 * answer than an empty box every time.
 */
export function applySettingDefaults(
  draft: ProviderDraft,
  specs: readonly ProviderSetting[],
): ProviderDraft {
  const settings = { ...draft.settings };
  let changed = false;
  for (const spec of specs) {
    if (!spec.default) continue;
    if (settings[spec.key] !== undefined && settings[spec.key] !== '') continue;
    settings[spec.key] = spec.default;
    changed = true;
  }
  return changed ? { ...draft, settings } : draft;
}

function num(value: string): number | undefined {
  const trimmed = value.trim();
  if (trimmed === '') return undefined;
  const n = Number(trimmed);
  return Number.isFinite(n) ? n : undefined;
}

/**
 * The request body.
 *
 * `complete` is for an edit: a PATCH leaves what it does not name alone, so a
 * field an operator cleared has to be sent as its empty value or clearing it
 * does nothing. The credential is the one exception in both directions -- an
 * empty one is never sent, because the server reads that as "keep the stored
 * one" and sending it would be the only way to make a blank box erase a
 * working credential.
 */
export function toProviderBody(
  draft: ProviderDraft,
  options: { complete?: boolean } = {},
): ProviderInput {
  const complete = options.complete === true;
  const body: ProviderInput = {
    name: draft.name.trim(),
    endpoint: draft.endpoint.trim(),
    insecure_skip_verify: draft.insecure_skip_verify,
    settings: trimmedMap(draft.settings),
    machine_labels: trimmedMap(draft.machine_labels),
    machine_capacity: num(draft.machine_capacity) ?? 2,
    machine_backend: draft.machine_backend as BackendKind,
    max_machines: num(draft.max_machines) ?? 0,
    max_creates_in_flight: num(draft.max_creates_in_flight) ?? 1,
    enabled: draft.enabled,
  };
  if (draft.kind !== '') body.kind = draft.kind as ProviderKindName;
  if (draft.credential.trim() !== '') body.credential = draft.credential;
  if (draft.ca_pem.trim() !== '' || complete) body.ca_pem = draft.ca_pem.trim();

  const platform: NonNullable<ProviderInput['machine_platform']> = {};
  if (draft.machine_os !== '') platform.os = draft.machine_os as NonNullable<typeof platform.os>;
  if (draft.machine_os_version.trim() !== '') platform.os_version = draft.machine_os_version.trim();
  if (draft.machine_arch !== '')
    platform.arch = draft.machine_arch as NonNullable<typeof platform.arch>;
  if (Object.keys(platform).length > 0 || complete) body.machine_platform = platform;

  assignNumber(body, 'machine_cpus', draft.machine_cpus, complete);
  assignNumber(body, 'machine_memory_mb', draft.machine_memory_mb, complete);
  assignNumber(body, 'machine_disk_mb', draft.machine_disk_mb, complete);
  assignNumber(body, 'cost_per_machine_hour', draft.cost_per_machine_hour, complete);

  const idle = draft.idle_timeout.trim();
  if (idle !== '' || complete) body.idle_timeout = idle;
  return body;
}

type NumericField =
  'machine_cpus' | 'machine_memory_mb' | 'machine_disk_mb' | 'cost_per_machine_hour';

function assignNumber(
  body: ProviderInput,
  field: NumericField,
  value: string,
  complete: boolean,
): void {
  const parsed = num(value);
  if (parsed !== undefined) body[field] = parsed;
  else if (complete) body[field] = 0;
}

function trimmedMap(map: Record<string, string>): Record<string, string> {
  const out: Record<string, string> = {};
  for (const [key, value] of Object.entries(map)) {
    const k = key.trim();
    if (k === '') continue;
    out[k] = value.trim();
  }
  return out;
}

/** A Go duration, as the API's Duration type spells one. */
const DURATION = /^\d+(\.\d+)?(ns|us|µs|ms|s|m|h)$/;

/**
 * What is wrong with the draft, keyed by the field name the API would use.
 *
 * Client rules only. Anything that needs the hypervisor -- whether the node
 * exists, whether the token may clone -- is the preflight's to answer, and
 * guessing at it here would produce a form that disagrees with the button
 * beside it.
 */
export function draftErrors(
  draft: ProviderDraft,
  specs: readonly ProviderSetting[],
  options: { editing?: boolean } = {},
): Record<string, string> {
  const out: Record<string, string> = {};
  if (draft.kind === '') out.kind = 'Choose which kind of provider this is.';
  if (draft.name.trim() === '') out.name = 'Give this provider a name you will recognise.';
  const endpoint = draft.endpoint.trim();
  if (endpoint === '') out.endpoint = 'Give the address this controller reaches the provider on.';
  else if (!/^https?:\/\//i.test(endpoint))
    out.endpoint = 'The address needs a scheme: https://pve.example.com:8006.';
  else if (/^http:\/\//i.test(endpoint) && !draft.insecure_skip_verify)
    out.endpoint =
      'Plain HTTP sends the credential in the clear. Use https, or say so deliberately below.';
  // An edit leaves the stored credential alone, so an empty box is an answer
  // there and a missing one only on the way in.
  if (!options.editing && draft.credential.trim() === '')
    out.credential = 'A credential is needed before anything can be created.';

  for (const spec of specs) {
    const value = (draft.settings[spec.key] ?? '').trim();
    const field = `settings.${spec.key}`;
    if (spec.required && value === '') {
      out[field] = `${spec.label} is needed.`;
      continue;
    }
    if (value !== '' && spec.kind === 'number' && num(value) === undefined) {
      out[field] = `${spec.label} has to be a number.`;
    }
  }

  const capacity = num(draft.machine_capacity);
  if (capacity === undefined || capacity < 1)
    out.machine_capacity = 'A machine has to offer at least one runner slot.';
  for (const [field, label] of [
    ['machine_cpus', 'vCPUs'],
    ['machine_memory_mb', 'Memory'],
    ['machine_disk_mb', 'Disk'],
    ['cost_per_machine_hour', 'Cost per machine-hour'],
  ] as const) {
    const raw = draft[field];
    if (raw.trim() === '') continue;
    const parsed = num(raw);
    if (parsed === undefined || parsed < 0) out[field] = `${label} has to be a number, or empty.`;
  }

  const max = num(draft.max_machines);
  if (max === undefined || max < 0 || !Number.isInteger(max))
    out.max_machines = 'A whole number of machines, or zero to rent nothing.';
  const inFlight = num(draft.max_creates_in_flight);
  if (inFlight === undefined || inFlight < 1)
    out.max_creates_in_flight = 'At least one, or nothing would ever be built.';
  const idle = draft.idle_timeout.trim();
  if (idle !== '' && !DURATION.test(idle)) out.idle_timeout = 'A duration like 15m, 1h or 90s.';
  return out;
}

/**
 * Whether this draft would rent anything at all, and why not.
 *
 * A provider saved with a ceiling of zero is a legal answer and the default
 * one, so this is a note beside the setting rather than an error on it.
 */
export function willRentNothing(draft: ProviderDraft): string {
  if (!draft.enabled) return 'This provider is switched off, so nothing will be built.';
  if ((num(draft.max_machines) ?? 0) === 0)
    return 'The ceiling is zero, so nothing will be built until it is raised.';
  return '';
}
