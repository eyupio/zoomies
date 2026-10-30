/**
 * What a person's bands look like, wherever the choice is kept.
 *
 * One shape serves both copies: the browser's localStorage, which is the
 * immediate fallback, and the account document, which is what carries a choice
 * from the desk to the phone. Reading is deliberately forgiving: a page, a cut
 * or a logo size this build no longer knows is dropped rather than failing, so
 * an operator who signs in to an older controller sees the bands it can draw
 * instead of none at all.
 */

import type { BandPreferences } from '../api/types';
import { isBandLogoSize, type BandLogoSize } from './band-logo';
import { isBandPage, isBandSize, type BandPage, type BandSize } from './pages';

export interface BandSettings {
  /** Pages whose band is switched off. The exception, so a band added later is on. */
  hidden: BandPage[];
  /** Page cuts that are not the page's default. */
  sizes: Partial<Record<BandPage, BandSize>>;
  /** Whether the dog is drawn by default. */
  logo: boolean;
  /** The dog's size by default; `auto` follows each band's cut. */
  logoSize: BandLogoSize;
  /** Pages that differ from `logo`. */
  logoPages: Partial<Record<BandPage, boolean>>;
  /** Pages that differ from `logoSize`. */
  logoSizes: Partial<Record<BandPage, BandLogoSize>>;
}

export const DEFAULT_BAND_SETTINGS: BandSettings = {
  hidden: [],
  sizes: {},
  logo: true,
  logoSize: 'auto',
  logoPages: {},
  logoSizes: {},
};

function pageList(value: unknown): BandPage[] {
  if (!Array.isArray(value)) return [];
  return [...new Set(value.filter(isBandPage))];
}

/** A map keyed by page, with the keys and the values this build can honour. */
function pageMap<T>(
  value: unknown,
  valid: (candidate: unknown) => candidate is T,
): Partial<Record<BandPage, T>> {
  const out: Partial<Record<BandPage, T>> = {};
  if (!value || typeof value !== 'object' || Array.isArray(value)) return out;
  for (const [page, entry] of Object.entries(value)) {
    if (isBandPage(page) && valid(entry)) out[page] = entry;
  }
  return out;
}

function isBoolean(value: unknown): value is boolean {
  return typeof value === 'boolean';
}

/** The settings a stored or received document describes, defaults for the rest. */
export function readBandSettings(raw: unknown): BandSettings {
  const doc = raw && typeof raw === 'object' ? (raw as Record<string, unknown>) : {};
  return {
    hidden: pageList(doc.hidden),
    sizes: pageMap(doc.sizes, isBandSize),
    logo: isBoolean(doc.logo) ? doc.logo : DEFAULT_BAND_SETTINGS.logo,
    logoSize: isBandLogoSize(doc.logo_size) ? doc.logo_size : DEFAULT_BAND_SETTINGS.logoSize,
    logoPages: pageMap(doc.logo_pages, isBoolean),
    logoSizes: pageMap(doc.logo_sizes, isBandLogoSize),
  };
}

/**
 * The account document for these settings.
 *
 * Every field is written, including the defaults, because the document is a
 * replacement: a page left out would otherwise be read as "never chosen" on the
 * next browser and fall back to a default the person had moved away from.
 */
export function bandSettingsDocument(settings: BandSettings): BandPreferences {
  return {
    hidden: [...settings.hidden],
    sizes: { ...settings.sizes },
    logo: settings.logo,
    logo_size: settings.logoSize,
    logo_pages: { ...settings.logoPages },
    logo_sizes: { ...settings.logoSizes },
  };
}
