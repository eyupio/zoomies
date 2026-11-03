/**
 * What a person's bands look like, wherever the choice is kept.
 *
 * One shape serves both copies: the browser's localStorage, which is the
 * immediate fallback, and the account document, which is what carries a choice
 * from the desk to the phone. Reading is deliberately forgiving: a page or a cut
 * this build no longer knows is dropped rather than failing, so
 * an operator who signs in to an older controller sees the bands it can draw
 * instead of none at all.
 */

import type { BandPreferences } from '../api/types';
import { isBandPage, isBandSize, type BandPage, type BandSize } from './pages';

export interface BandSettings {
  /** Pages whose band is switched off. The exception, so a band added later is on. */
  hidden: BandPage[];
  /** Page cuts that are not the page's default. */
  sizes: Partial<Record<BandPage, BandSize>>;
}

export const DEFAULT_BAND_SETTINGS: BandSettings = {
  hidden: [],
  sizes: {},
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

/** The settings a stored or received document describes, defaults for the rest. */
export function readBandSettings(raw: unknown): BandSettings {
  const doc = raw && typeof raw === 'object' ? (raw as Record<string, unknown>) : {};
  return {
    hidden: pageList(doc.hidden),
    sizes: pageMap(doc.sizes, isBandSize),
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
  };
}
