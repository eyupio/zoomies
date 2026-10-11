# Zoomies branding guide v3.0

The approved master is the running cocker spaniel inside a circular swish,
with the Zoomies wordmark, selected by
the owner on 10 October 2026. It replaces the previous curled-dog artwork.

## Artwork

Use `logo-black-transparent.svg` on a pale backdrop in both themes. The
original black coat and facial markings must not be inverted. The white
exports remain available as legacy assets, but the web UI does not select them
for dark mode. Matching PNG exports are available. These SVGs contain vector
paths, not embedded bitmap images.

The emblem files are `mark-black-transparent.svg` and
`mark-white-transparent.svg`, with matching PNGs. They retain the exact dog
paths from the full logo, with no lettering, circle or detached speed strokes.
The wordmark SVGs contain the lettering paths from the approved master.

Use the existing `paw-swish-black.png` with the selected pale backdrop at 16 to 64px.
These original small-size assets are unchanged. Do not substitute a dog head.

## Placement

Give the full logo a nominal width of at least 220px and the standalone emblem
at least 128px where space permits. On narrow screens scale the complete logo
and backdrop together to fit, without clipping the atmosphere. Preserve the supplied canvas and aspect ratio.
Leave clear space equal to the wordmark's O around the full logo, or 12.5%
of the emblem's width. Never stretch, recolour individual parts, add shadows,
or replace the lettering with a font. Keep the original black artwork in both themes. Settings, Appearance offers
Starlight (the default), Pearl veil, Orbital haze, White square and White circle.
Atmospheric treatments blend pale light into the surrounding surface; stars and
orbital trails belong to the backdrop behind the separate original artwork.
Simple treatments use a white inset.
The backdrop may have a fine edge or subtle shadow; never filter the artwork.
Paw icons keep their original dimensions and theme variants, with no backdrop.
The full logo remains visible above the form on a portrait phone.

## Colour and typography

The identity stays monochrome. Interface colours and typography remain in
[the brand reference](../brand.md) and `brand-tokens.json`. Inter is interface
text, not a substitute for the drawn wordmark.

## Sources and exports

`ASSET_MANIFEST.json` records provenance and SHA-256 checksums. The canonical
SVG files live here; runtime copies live in `web/public/brand`. PNGs and social
images are exported from those vectors. The previous PDF guide is retired.

The descriptor has been removed from the artwork. The lower ring is a constant-width circular arc, with clearance behind the illustration and wordmark.
