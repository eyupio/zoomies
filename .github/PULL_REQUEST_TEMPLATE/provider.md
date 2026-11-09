<!--
Use this template when a pull request adds or changes a hosting provider: a
platform or service that deploys a Zoomies controller or hosts runners. GitHub
has no picker for pull request templates, so open the pull request with
`template=provider.md` in the compare URL. Every other pull request uses the
default template. CONTRIBUTING.md has the detail. Delete a heading that does
not apply.

A provider is a third party. This template asks for what a reader needs to
decide whether to trust one; filling it in is not an endorsement.
-->

## The provider

- Name and link:
- What the route installs: a controller, a runner host or both, and whether the
  machine it creates also runs jobs itself.
- Your relationship to the provider: none, a customer, an employee, the
  template's maintainer, or a referral or affiliate arrangement. Say so even
  when the answer is none.

## What you checked

"Checked" means you deployed it and followed the guide you wrote, not that you
read the provider's listing. Say where each answer came from.

| | Answer | Where you checked it |
| --- | --- | --- |
| Zoomies release deployed, and the date | | |
| The image, and whether it is pinned to a release tag or a digest | | |
| Who maintains the template | | |
| How a new Zoomies release reaches it | | |
| Who operates the instance | | |
| The provider's privacy notice and terms (links) | | |

## The approach

Anything left out deliberately, and why. A provider whose template needs a
credential typed into its form, or keeps one in instance metadata, will not be
listed: say how yours avoids it.

## What you ran

The checks you ran and the ones you could not, stated exactly. `make test-short`
and `make lint` are the usual minimum, and `mkdocs build --strict` for the page.

## Impact

- [ ] A guide page in `docs/`, modelled on `docs/instapods.md`, with its entry in
      the `nav` of `mkdocs.yml`, and no placeholder left in it.
- [ ] The page says what the route installs and what the reader still has to do
      afterwards: the setup token, the first administrator, connecting GitHub, a
      runner host and a pool.
- [ ] The page names who operates the instance, links the provider's privacy
      notice and terms, and does not suggest the Zoomies project operates,
      supports or has audited it.
- [ ] The Zoomies release it was checked against, and the date, are on the page.
- [ ] Links to the provider carry no referral or affiliate parameter, or the page
      says they do.
- [ ] No image is loaded from the provider's site or CDN. A button or logo is a
      file in the repository, used with the provider's permission.
- [ ] The README gains at most one sentence of prose that links to the page, and
      the page is listed wherever the other routes are.
- [ ] No credentials, tokens, local databases or build output committed.
