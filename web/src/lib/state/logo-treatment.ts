/** The backdrop changes; the approved black artwork does not. */
export const LOGO_TREATMENTS = ['starlight', 'pearl', 'orbital', 'square', 'circle'] as const;
export type LogoTreatment = (typeof LOGO_TREATMENTS)[number];
export const DEFAULT_LOGO_TREATMENT: LogoTreatment = 'starlight';

export const LOGO_TREATMENT_OPTIONS = [
  {
    value: 'starlight',
    label: 'Starlight',
    description: 'Warm clouds of light, dense stars and delicate constellations.',
  },
  {
    value: 'pearl',
    label: 'Pearl veil',
    description: 'Flowing pearl and silver mist with luminous wisps.',
  },
  {
    value: 'orbital',
    label: 'Orbital haze',
    description: 'Platinum clouds crossed by luminous blue orbital trails.',
  },
  {
    value: 'square',
    label: 'White square',
    description: 'A simple white inset with softly rounded corners.',
  },
  {
    value: 'circle',
    label: 'White circle',
    description: 'A simple white inset that follows the logo’s ring.',
  },
] as const;

export function isLogoTreatment(value: unknown): value is LogoTreatment {
  return LOGO_TREATMENTS.some((treatment) => treatment === value);
}

export function resolveLogoTreatment(value: unknown): LogoTreatment {
  return isLogoTreatment(value) ? value : DEFAULT_LOGO_TREATMENT;
}
