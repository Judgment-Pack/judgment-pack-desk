# Google registration setup: one working surface

Reviewed September 21, 2026. This document recommends a layout change; the
associated wording correction does not yet change the setup interaction.

## Current friction

Admin → Connections currently opens a registration modal. Guide dismisses that
modal and opens a right-hand guide. Continue setup closes the guide and reopens
the modal, where the credentials file can finally be selected. These are two
surfaces for one task, with separate focus and scroll transitions. The path to
the file picker through the guide requires four Desk actions: Set up, Guide,
Continue setup, Choose credentials file.

The guide also combined Branding and Audience in one paragraph. They are
separate Google Auth Platform pages. Personal account is not a Google UI option.
External supports personal Google accounts; Internal is limited to the project's
Google Workspace organization. Testing needs a test-user list and gives these
connections a seven-day refresh-token lifetime. An existing In production
configuration can be retained for personal local use; personal-use verification
exceptions and unverified-app warnings still apply.

## Recommendation

Set up and Manage should open a single provider setup pane, reusing the shell's
resizing, sticky header, independent scrolling and focus restoration. Start at
480 px, clamp within the existing shell geometry, and use the existing drawer
on narrow screens. Keep the provider rows visible on wide screens. Set up opens
the pane explicitly; background state changes should not open it.

- Header: Set up Google Drive or Set up Gmail, with the existing close control.
- Keep one file-selection action readily available in a sticky footer. Label it
  Upload credentials JSON, with nearby text explaining that the file is validated
  and saved on this computer. Users who already have the file need not read all
  instructions first.
- Show Setup instructions expanded for a provider that needs registration;
  collapse them when managing an existing registration. The disclosure opens
  directly below its toggle and can be closed without leaving setup. Keep upload
  outside the disclosure so it is always available. Keep all instructions
  in this pane, with the official Google documentation links after the steps.
- Use the exact Google navigation labels. Explain Testing versus In production
  in context, without making Testing mandatory or promising permanent tokens.
- Show upload progress, errors, and Registration saved in the same pane. Do not
  close it automatically on success. Save registration is separate from account
  consent: registration success must not be labeled Connected or start OAuth.
- Retain Return to chat when setup was opened from chat. Otherwise Done closes
  the pane; the next sign-in remains available through the chat attachment menu.
- For an already connected account, keep the existing instruction to disconnect
  in My connections before replacing its registration. Do not offer a writable
  upload control in that state.

This removes Guide and Continue setup from the path to upload: Set up → Upload
credentials JSON. Google Cloud configuration and consent still require the
user's actions; Desk cannot infer that external steps have been completed.

## Alternatives

| Option | Fit for this task |
| --- | --- |
| Combined setup pane | Recommended: one place for instructions and upload, consistent with Desk's contextual panes; supports repeat use with a direct upload action. |
| Expand setup below the provider row | Good for a short form. Six detailed Google steps make the page long and push the next provider away, especially in translated layouts. |
| Dedicated provider setup page | Gives a complex workflow more room and a shareable URL. Prefer if setup grows into multiple accounts, organization policies, or many configuration fields. Currently adds navigation for a small form. |
| Keep a modal and move all help inside it | Removes the round trip but keeps a long reference task inside a blocking surface. Weakest fit for users switching between Google Cloud and Desk. |

## Evidence and limits

[Linear's 2026 design refresh](https://linear.app/now/behind-the-latest-design-refresh)
emphasizes predictable placement, visual hierarchy and quieter supporting
controls. This supports consolidating the actions; it does not prescribe a
particular OAuth setup layout.

[NN/g's modal/nonmodal guidance](https://www.nngroup.com/articles/modal-nonmodal-dialog/)
advises against blocking dialogs for tasks requiring additional reference
information. [Material's official side-sheet component documentation](https://github.com/material-components/material-components-android/blob/master/docs/components/SideSheet.md)
distinguishes standard and modal sheets. Desk can use a standard dock on desktop
and its accessible drawer when there is insufficient space.

Google's [consent setup](https://developers.google.com/workspace/guides/configure-oauth-consent),
[publishing states](https://developers.google.com/identity/protocols/oauth2/production-readiness/overview),
[personal-use exception](https://developers.google.com/identity/protocols/oauth2/production-readiness/restricted-scope-verification#personal-use),
and [token expiration rules](https://developers.google.com/identity/protocols/oauth2#expiration)
support the revised copy.

This recommendation comes from reviewing the actual component flow and public
design guidance, not a comparative user study. Before shipping the pane change,
check both providers in setup-required, configured, connected, unavailable,
uploading, failed, and saved states. Verify closing, Escape, provider switching,
late responses, keyboard focus, narrow drawers, long translations, and resizing.
Keep credential values out of rendered output, logs, and translations.
