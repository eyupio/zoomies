export type UIStyle = 'classic' | 'mission-control';

export const UI_STYLE_KEY = 'zoomies.ui-style';

export const UI_STYLE_OPTIONS: readonly { value: UIStyle; label: string; description: string }[] = [
  {
    value: 'classic',
    label: 'Classic',
    description: 'The familiar Zoomies palette and compact type.',
  },
  {
    value: 'mission-control',
    label: 'Mission Control',
    description: 'Navy surfaces, cool blue controls and roomier type. Same Zoomies personality.',
  },
];

/**
 * Mission Control is the default: only a stored, deliberate `classic` opts a
 * browser out. Unknown values fall back to the default rather than to an
 * accident of an older build's storage.
 */
export function resolveUIStyle(value: unknown): UIStyle {
  return value === 'classic' ? value : 'mission-control';
}
