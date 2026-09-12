# Android GitHub setup validation

The dashboard submits the App manifest directly to GitHub with a native POST
form in the current tab. App installation uses a native GET form, preserving
the installation URL's query parameters as hidden controls. Neither button
uses a popup, JavaScript navigation, or the legacy controller 307 handoff.

Why: [Chromium's external-navigation documentation](https://chromium.googlesource.com/chromium/src/+/lkgr/components/external_intents/)
distinguishes ordinary form submissions from redirected submissions: the latter
can launch another app. `target="_self"` alone is not an Android intent control.
GitHub's own redirects and other browsers' policies can still switch apps.

The controller's CSP form allowlist and authenticated code exchange are unchanged.
The legacy handoff endpoint remains available for already-loaded older dashboards.
Recovery never transfers Zoomies session credentials or App private keys between
browsers. A pasted callback URL supplies only the code and state to the normal
authenticated exchange endpoint; it is not fetched or navigated to. Pending
setups still expire after an hour and do not survive a controller restart.

## Automated checks

Build the UI and controller, then run:

```sh
cd web
npx playwright test tests/github-setup.spec.ts --project=chromium --project=mobile
```

The tests intercept GitHub navigation and assert the actual POST body, state,
GET query values (including duplicates), absence of an intermediary redirect,
single tab, reload recovery, and callback-URL exchange without saved progress.
Pixel emulation does **not** exercise Android's OS-level intent dispatcher.

## Physical-device release check

1. On Android Chrome and Firefox, log into Zoomies and GitHub in the same browser.
   Test with the GitHub app installed and with its link handling disabled.
2. Connect a disposable GitHub App. Confirm Create the App reaches GitHub with
   the manifest prefilled, without Zoomies opening a new browser or app chooser.
3. Confirm the callback exchanges the code and offers Install it. Install on
   the intended account, return, and finish the Zoomies connection.
4. Reload between creation and installation; confirm the existing App is resumed.
5. If a browser does switch apps, copy the return URL back to the original
   Zoomies dialog. Use Code from GitHub or Installation ID as appropriate.
   Treat the callback URL as sensitive; do not post it in issues or logs.
6. Repeat with an expired setup; verify the normal expiry error is shown rather
   than bypassing state validation. Remove the disposable App after testing.
