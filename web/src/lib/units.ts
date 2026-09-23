/**
 * Sizes as a person writes them, and as the page writes them back.
 *
 * Pure, and imported by a unit test. A size field used to be a number box with
 * the unit in its label -- "Memory (MB)" -- so the operator who thinks in
 * gigabytes did the arithmetic, and the one who typed "4g" the way Docker
 * spells it was told nothing at all, because a number box reports anything it
 * cannot read as an empty string. Every size field now reads the spellings
 * people actually use -- 4gb, 4096mb, 4g, 4 GiB, 1.5 -- and, once the value is
 * committed, writes it back in the largest unit that says it exactly, so the
 * field shows what was understood rather than what was typed.
 *
 * A gigabyte here is 1024 megabytes. That is what the rest of the UI means by
 * GB, what Docker means by `4g`, and what a cgroup is given; offering a decimal
 * gigabyte beside it would make "4 GB" mean two different limits on one page.
 */

/** A quantity a size field can hold, named by the unit the API carries it in. */
export type Quantity = 'cpus' | 'mb' | 'gb';

/** What a parse makes of some text: a value, nothing (the field was cleared), or why not. */
export type Parsed = { ok: true; value: number | null } | { ok: false; error: string };

const MB_PER: Record<string, number> = {
  k: 1 / 1024,
  m: 1,
  g: 1024,
  t: 1024 * 1024,
  p: 1024 * 1024 * 1024,
};

/**
 * Reads a byte size into megabytes. A bare number is taken in `bare`, the unit
 * the field is labelled in, so "4096" in a megabyte field stays 4096 MB and "4"
 * in a gigabyte field stays 4 GB.
 */
function parseBytes(text: string, bare: 'mb' | 'gb'): number | undefined {
  const m = /^(\d+(?:\.\d+)?|\.\d+)\s*([kmgtp]?)(?:i?b)?$/i.exec(text);
  if (!m) return undefined;
  const n = Number(m[1]);
  const unit = (m[2] ?? '').toLowerCase();
  if (unit === '') {
    // "4b" would read as four megabytes, which nobody meant.
    if (/b$/i.test(text)) return undefined;
    return bare === 'gb' ? n * 1024 : n;
  }
  return n * (MB_PER[unit] ?? NaN);
}

/**
 * Reads a CPU figure. Cores are the unit, and fractions are allowed: "1.5",
 * "1.5 cores", "2 vCPUs". Millicores are read too, because "1500m" is how
 * anyone who has written a Kubernetes manifest spells one and a half cores.
 */
function parseCpus(text: string): number | undefined {
  const milli = /^(\d+)\s*m$/i.exec(text);
  if (milli) return Number(milli[1]) / 1000;
  const m = /^(\d+(?:\.\d+)?|\.\d+)\s*(?:v?cpus?|cores?|c)?$/i.exec(text);
  return m ? Number(m[1]) : undefined;
}

const EXAMPLES: Record<Quantity, string> = {
  cpus: 'a number of cores, such as 2 or 1.5',
  mb: 'a size such as 4 GB, 4096 MB or 4g',
  gb: 'a size such as 40 GB, 1.5 TB or 500g',
};

/**
 * Reads what somebody typed into a size field, in the field's own unit.
 *
 * Megabytes and gigabytes come back whole, because that is what the API
 * carries: "1.5g" in a megabyte field is 1536, and a gigabyte field rounds to
 * the nearest gigabyte. The rounding is visible, because the field then shows
 * the value it kept. CPU keeps two decimal places, finer than any quota a
 * runtime applies.
 */
export function parseQuantity(input: string, quantity: Quantity): Parsed {
  const text = input
    .trim()
    .replace(/[\s_]+/g, ' ')
    .replace(/,/g, '');
  if (text === '') return { ok: true, value: null };
  let value: number | undefined;
  if (quantity === 'cpus') {
    const cpus = parseCpus(text);
    value = cpus === undefined ? undefined : Math.round(cpus * 100) / 100;
  } else {
    const mb = parseBytes(text, quantity);
    if (mb !== undefined) value = quantity === 'gb' ? Math.round(mb / 1024) : Math.round(mb);
  }
  if (value === undefined || !Number.isFinite(value) || value < 0) {
    return { ok: false, error: `Write ${EXAMPLES[quantity]}.` };
  }
  return { ok: true, value };
}

const BYTE_UNITS = ['MB', 'GB', 'TB', 'PB'] as const;

/**
 * Writes a size in the largest unit that says it exactly, to two decimal
 * places: 4096 MB is "4 GB", 1536 MB is "1.5 GB", and 3000 MB stays "3000 MB",
 * because "2.93 GB" would be a different limit from the one that is set.
 */
export function formatQuantity(value: number, quantity: Quantity): string {
  if (!Number.isFinite(value)) return '';
  if (quantity === 'cpus') return `${trim(value)} ${value === 1 ? 'core' : 'cores'}`;
  let at = quantity === 'gb' ? 1 : 0;
  let n = value;
  while (at < BYTE_UNITS.length - 1 && n >= 1024 && exact(n / 1024)) {
    n /= 1024;
    at++;
  }
  return `${trim(n)} ${BYTE_UNITS[at]}`;
}

/** Whether a figure survives being written to two decimal places. */
function exact(n: number): boolean {
  return Math.abs(Math.round(n * 100) - n * 100) < 1e-6;
}

function trim(n: number): string {
  return String(Math.round(n * 100) / 100);
}
