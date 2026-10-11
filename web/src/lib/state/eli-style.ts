/**
 * How Eli answers this person: how long, and for whom. It is a preference of
 * the reader and not of the provider, so it lives with the other per-operator
 * choices, is synced to the account where there is one, and goes to the
 * controller with every question, which writes it into the model's prompt.
 */

export const ELI_LENGTHS = ['short', 'medium', 'long'] as const;
export type EliLength = (typeof ELI_LENGTHS)[number];
export const ELI_TONES = ['plain', 'balanced', 'technical'] as const;
export type EliTone = (typeof ELI_TONES)[number];

export interface EliStyle {
  length: EliLength;
  tone: EliTone;
}

/**
 * What a person who has not chosen gets: the answer and what to do, under a
 * couple of hundred words, written for an operator. It is the answer that fits
 * on a phone, which is where the long ones were being read.
 */
export const DEFAULT_ELI_STYLE: EliStyle = { length: 'short', tone: 'balanced' };

export const ELI_LENGTH_OPTIONS = [
  {
    value: 'short',
    label: 'Short',
    name: 'The answer and what to do, under about two hundred words',
  },
  { value: 'medium', label: 'Medium', name: 'The answer with its reasons and evidence' },
  { value: 'long', label: 'Long', name: 'The full picture, including what was ruled out' },
] as const;

export const ELI_TONE_OPTIONS = [
  { value: 'plain', label: 'Plain', name: 'Terms explained, no commands unless asked' },
  {
    value: 'balanced',
    label: 'Balanced',
    name: 'Plain words with the exact names of what to act on',
  },
  {
    value: 'technical',
    label: 'Technical',
    name: 'Exact names and figures, common terms not explained',
  },
] as const;

export function isEliLength(value: unknown): value is EliLength {
  return ELI_LENGTHS.some((v) => v === value);
}

export function isEliTone(value: unknown): value is EliTone {
  return ELI_TONES.some((v) => v === value);
}

/**
 * The stored style, each half validated on its own: a value this build does
 * not offer, or nothing stored, is a person who has not chosen that half and
 * gets its default, while the half they did choose is kept.
 */
export function resolveEliStyle(
  stored: { length?: unknown; tone?: unknown } | undefined,
): EliStyle {
  return {
    length: isEliLength(stored?.length) ? stored.length : DEFAULT_ELI_STYLE.length,
    tone: isEliTone(stored?.tone) ? stored.tone : DEFAULT_ELI_STYLE.tone,
  };
}
