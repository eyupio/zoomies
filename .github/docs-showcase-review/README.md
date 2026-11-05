# Documentation showcase review

Screenshots of the implemented MkDocs site, captured from a strict local build.
The product screens contain the repository's existing example fleet data.
Repository release facts are omitted when the local build cannot reach GitHub;
the publishing workflow requires them before deployment.

- `home-desktop.png`: 1440 px homepage, first viewport.
- `documentation-desktop.png`: 1440 px documentation hub, first viewport.
- `home-mobile.png`: 390 px homepage, first viewport.
- `navigation-mobile.png`: 390 px navigation with a persistent close control.

The browser checks in `hooks/tests/browser.mjs` can also capture full layouts when
`DOCS_SCREENSHOTS` names an output directory. They cover search, themes, native
navigation, existing deep links, filtering, Mermaid and responsive reading.
