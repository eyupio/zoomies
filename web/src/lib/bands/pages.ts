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
  { key: 'repairs', label: 'Eli repairs' },
  { key: 'migrate', label: 'Migrate' },
  { key: 'kennel', label: 'Kennel Club' },
] as const;

export type BandPage = (typeof BAND_PAGES)[number]['key'];

export function isBandPage(value: unknown): value is BandPage {
  return BAND_PAGES.some((page) => page.key === value);
}

/** How tall a band stands: Standard is the Overview's, Compact the other pages'. */
export const BAND_SIZES = [
  { key: 'compact', label: 'Compact' },
  { key: 'standard', label: 'Standard' },
] as const;

export type BandSize = (typeof BAND_SIZES)[number]['key'];

export function isBandSize(value: unknown): value is BandSize {
  return BAND_SIZES.some((size) => size.key === value);
}

/**
 * The size a page's band has until somebody chooses another. The Overview is the
 * front door and keeps its full stage; every page under it is a working page, where
 * the rows are the point, so its band is the slim one.
 */
export function defaultBandSize(page: BandPage): BandSize {
  return page === 'overview' ? 'standard' : 'compact';
}
