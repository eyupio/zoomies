/**
 * Whether a band draws its dog, and how large it is when it does.
 *
 * The dog's size is separate from the band's cut: the cut is about air and
 * type, the dog's size is about the brand. `auto` follows the cut, so a slim
 * band draws a smaller dog than an expanded one; the three named sizes pin it
 * whichever cut the band takes, because an operator who wants the numbers up
 * front may still want the brand large beside them.
 */

export const BAND_LOGO_SIZES = ['auto', 'small', 'medium', 'large'] as const;
export type BandLogoSize = (typeof BAND_LOGO_SIZES)[number];

export const DEFAULT_BAND_LOGO_SIZE: BandLogoSize = 'auto';

export const BAND_LOGO_SIZE_OPTIONS = [
  { value: 'auto', label: 'Auto', name: 'Follows the band size' },
  { value: 'small', label: 'Small', name: 'A smaller dog' },
  { value: 'medium', label: 'Medium', name: 'The middle dog' },
  { value: 'large', label: 'Large', name: 'A larger dog' },
] as const;

export function isBandLogoSize(value: unknown): value is BandLogoSize {
  return BAND_LOGO_SIZES.some((size) => size === value);
}

/**
 * The sizes BrandStage actually draws. `auto` is a preference, resolved here to
 * an explicit width by PageBand, so the stage itself only ever hears a size.
 */
export type BandLogoArtSize = Exclude<BandLogoSize, 'auto'>;
