/**
 * The pages that open on a band, in the order Appearance lists them. A page not
 * listed here has no band to switch off. A page with a `picture` draws one
 * beside its sentence, where the dog stands, and Appearance offers to switch
 * it off; the words say what the picture is.
 */
export const BAND_PAGES = [
  { key: 'overview', label: 'Overview', picture: 'the week by the hour' },
  { key: 'runners', label: 'Runners', picture: 'the slots by host' },
  { key: 'pools', label: 'Pools' },
  { key: 'queue', label: 'Queue' },
  { key: 'jobs', label: 'Jobs' },
  { key: 'workflows', label: 'Workflows' },
  { key: 'usage', label: 'Usage' },
  { key: 'hosts', label: 'Hosts', picture: 'the slots by host' },
  { key: 'providers', label: 'Providers' },
  { key: 'installations', label: 'Installations' },
  { key: 'migrate', label: 'Migrate' },
  { key: 'kennel', label: 'Kennel Club' },
] as const;

export type BandPage = (typeof BAND_PAGES)[number]['key'];

/** The pages whose band draws a picture, so a switch for one is offered. */
export const BAND_PICTURE_PAGES = BAND_PAGES.filter(
  (page): page is Extract<(typeof BAND_PAGES)[number], { picture: string }> => 'picture' in page,
);

export function isBandPage(value: unknown): value is BandPage {
  return BAND_PAGES.some((page) => page.key === value);
}

/**
 * How tall a band stands, from the shortest cut to the tallest. The order is the
 * order the choices appear in, so the control reads as a scale rather than a list.
 */
export const BAND_SIZES = [
  { key: 'slim', label: 'Slim', name: 'The shortest cut, for a page with rows to show' },
  { key: 'compact', label: 'Compact', name: 'The working pages cut' },
  { key: 'standard', label: 'Standard', name: 'The Overview cut' },
  { key: 'expanded', label: 'Expanded', name: 'The tallest cut, for a page with room' },
] as const;

export type BandSize = (typeof BAND_SIZES)[number]['key'];

export function isBandSize(value: unknown): value is BandSize {
  return BAND_SIZES.some((size) => size.key === value);
}

/**
 * The size a page's band has until somebody chooses another. Every page opens
 * on the shortest cut, the Overview included: its hero is a summary rather than
 * the page's subject, and a working page's first row must stay above a 900px
 * window's fold.
 */
export function defaultBandSize(): BandSize {
  return 'slim';
}
