/**
 * The pages that open on a band, in the order Appearance lists them. A page not
 * listed here has no band to switch off.
 */
export const BAND_PAGES = [
  { key: 'overview', label: 'Overview' },
  { key: 'runners', label: 'Runners' },
  { key: 'pools', label: 'Pools' },
  { key: 'queue', label: 'Queue' },
  { key: 'jobs', label: 'Jobs' },
  { key: 'workflows', label: 'Workflows' },
  { key: 'usage', label: 'Usage' },
  { key: 'hosts', label: 'Hosts' },
  { key: 'providers', label: 'Providers' },
  { key: 'installations', label: 'Installations' },
  { key: 'migrate', label: 'Migrate' },
  { key: 'kennel', label: 'Kennel Club' },
] as const;

export type BandPage = (typeof BAND_PAGES)[number]['key'];

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
 * The size a page's band has until somebody chooses another. The Overview is the
 * front door and keeps its full stage; every page under it is a working page,
 * where the rows are the point, so its band is the shortest cut. Compact was the
 * default and still left the first row of Runners and Queue below a 900px
 * window's fold, which is the one thing a list page may not do.
 */
export function defaultBandSize(page: BandPage): BandSize {
  return page === 'overview' ? 'standard' : 'slim';
}
