/** One choice controls the vocabulary and the illustration across all status surfaces. */
export const STATUS_STYLES = ['off', 'cute', 'standard'] as const;
export type StatusStyle = (typeof STATUS_STYLES)[number];

export const STATUS_STYLE_OPTIONS = [
  { value: 'off', label: 'Off', name: 'Plain status names and icons' },
  { value: 'cute', label: 'Cute', name: 'Zoomies words and the original animated dog' },
  { value: 'standard', label: 'Standard', name: 'Zoomies words and the monochrome spaniel' },
] as const;

export function isStatusStyle(value: unknown): value is StatusStyle {
  return STATUS_STYLES.some((style) => style === value);
}

/** Preserve the old switch, including an explicit opt-out, when upgrading. */
export function resolveStatusStyle(stored: {
  statusStyle?: unknown;
  quirkyStatus?: unknown;
}): StatusStyle {
  if (isStatusStyle(stored.statusStyle)) return stored.statusStyle;
  return stored.quirkyStatus === false ? 'off' : 'cute';
}
