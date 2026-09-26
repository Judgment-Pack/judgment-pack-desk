# Authentication design review

Status: the local OIDC sign-in page and Admin setup pane are implemented.
The older static mocks below are design history, including the hosted screens;
they do not describe the current local entry flow. All production strings are
localized. Hosted sign-in and multi-user workspaces remain out of scope.

## Implemented local flow

Open Desk's plain localhost URL. With the backend environment option
`JPACK_DESK_LOCAL_ACCESS=1`, an unconfigured personal installation opens directly
with an expiring local session. A reload reuses that session. End session removes
access from the current page; Continue or a reload opens another local session.
The option never overrides an enabled or unreadable identity policy. Without
this opt-in, an unconfigured installation offers owner setup, requiring the code
printed by the running process. Admin → Sign-in & access opens
a right pane with registration fields and expandable instructions. Test returns
to that pane with the verified identity. Explicit activation makes that account
the sole owner and ends previous sessions. Ordinary visits then offer the
configured provider, redirect externally, and return to the workspace. Errors,
cancellation and wrong-account refusals remain on the entry page.

No token is placed in the return URL; completion needs the proof held by the
initiating tab. Source connections are independent. See the README security model
for the private policy, expiry and local reset behavior.

The complete browser flow was exercised against a local signing OIDC fixture:
setup → test → account review → enable → wrong account refused → owner login →
logout and backend revocation. Entry layouts passed at 1440, 390 and 320 pixels.
This is protocol/UI acceptance, not a live Google or Entra registration test.

## Original design proposal

## Open the mock

With the existing Vite development server running, visit
[the preview](http://localhost:5173/auth-design.html#screen=local).
No launch URL, project session or credentials are required.

- [Local recovery](http://localhost:5173/auth-design.html#screen=local)
- [Hosted sign-in](http://localhost:5173/auth-design.html#screen=signin)
- [Admin setup](http://localhost:5173/auth-design.html#screen=setup)

The top screen/theme switcher and bottom preview notice are review tools, not
product chrome. The preview uses Desk's real Button, Input, Disclosure, glyphs,
font and palette. No AppShell/session/config providers are mounted. It reads or
writes no project/browser credential storage and calls no APIs. The default
production Vite entry does not include this page. Actions update an explicit
preview notice; they do not simulate a successful test or authenticated session.
Configuration fields are read-only example values. The documentation link is
the only intentional external navigation. Provider buttons use text while official
brand assets and branding compliance are reserved for implementation.

## Three screens

### 1. Local session recovery

Normal launch remains automatic: trusted local launcher → temporary handoff →
authorized session → workspace. A login page should not interrupt that path.

If the browser has no usable session, show **Open your local Desk**, a short
explanation, and **How to reopen Desk**. Its steps expand directly below.
This button is help, not authorization; it never grants access or sends a
credential. No cloud provider buttons, fabricated email identity or password form
appear. The implementation must distinguish session loss from unavailable local
storage before asserting saved files are present; this mock depicts the usual
session-loss case with storage intact. Recovery instructions should reflect the
actual launcher available (installed app or terminal), not invent an app installer.

### 2. Hosted sign-in

Use a centered, narrow form with quiet brand, one heading and consistent controls.
Show only methods supported by the configured issuer/broker. The mock illustrates
a future broker offering Google, Microsoft and company SSO; it is not a claim that
the current nullable provider schema supports several independent issuers.

Company SSO replaces the central form with workspace-address entry. A direct
workspace invitation/deep link bypasses this extra lookup. Lookup resolves only
trusted configured workspace identifiers; typed text never becomes an arbitrary
issuer URL. Return paths must be internal and authorized after sign-in.

The initial proposal has no native password, email-code or passkey form. Those
capabilities can be exposed when the chosen provider supplies them. Provider
credentials are entered on the provider's own pages. Registration and acceptance
of an invitation are separate from sign-in; don't promise open registration until
the hosted product's admission policy is decided.

Use equivalent layouts for callback progress, cancellation, expired session,
provider outage and access denied. Cancellation offers retry or another supported
method. Access denied offers a safe switch-account/help path; it never silently
creates membership. Sign-out and disconnect are distinct actions.

### 3. Admin → Sign-in & access

Evolve the existing Identity provider section. Connections continues to own
Drive/Gmail/source setup. The Admin screenshot depicts an example deployment being
prepared by its operator, not an anonymously accessible live hosted workspace.

The main pane summarizes provider and activation state. Set up opens the
contextual right pane containing display name, issuer, client ID and redirect URL.
Instructions expand below these fields; no extra Guide dialog. Confidential
credential provisioning stays a deployment concern pending the custody decision.

Flow: **Configure → Test sign-in → Review identity/access → Enable**. The mock
shows only Configure, so Test is a preview action and does not turn on sign-in.
Implementation must return test results to the initiating administrator session,
not replace that identity with the test account. Enable requires a successful test,
an explicit allowed-user/workspace policy and a verified recovery path. An
unconfigured network deployment must not serve the ordinary application.

Close returns focus to Set up. On narrow screens the pane occupies the workspace
content area and Close returns to settings; it is not layered over a second form.
Long instructions scroll within the pane while its actions remain accessible.

## Layout and accessibility

- Entry form max-width 384px; existing type/palette/radius tokens.
- Entry action targets at least 44px high; normal compact Admin controls remain.
- No ornamental illustration, stacked cards or workspace sidebars on entry.
- Responsive at 390px and 1440px; theme follows system with a review override.
- Semantic headings/labels, native disclosure, visible keyboard focus and a
  live preview-status announcement. No status communicated only through color.
- Shipped language selector uses the existing locale mechanism before sign-in.
  Preserve explicit language/theme choice across redirects without using an
  authenticated project's private preference store.

## Architecture and implementation scope

[ADR 0007](../adr/0007-authentication-and-identity-boundary.md) records provider
reuse, source-connection separation, repository ownership, local-data migration,
session design, hosted prerequisites and acceptance cases. It remains proposed.

The first implementation adds the production session gate, recovery screen and
confirmed End session action. The backend revokes only the presented session and
cancels its request contexts; the browser unmounts private views and clears its
query cache. Saved project/chat data and other sessions remain intact. Retry
handles an unavailable backend without inventing access. Reopening help is not
an authentication endpoint.

OIDC, provider activation, automatic session expiry, ownership migration and
hosted deployment remain later milestones. Existing source-connection OAuth does
not authenticate a Desk user. The application still binds loopback.

## Review evidence

Checked 2026-09-23 using Chrome through Playwright, with a fresh browser context:

- TypeScript check passed (`npm --prefix web run typecheck`).
- All three screens rendered at 1440 × 960 in dark and light themes, and at
  390 × 844 and 320 × 844 in dark theme: 12 layouts, no horizontal overflow.
- Reopening steps, inline setup instructions and the company SSO form work.
- Closing setup or pressing Escape restores focus to Set up; reopening focuses
  the pane heading. Narrow layout returns to settings after closing setup.
- Provider/test actions announce their preview status. No application API,
  launch or external requests; no browser errors or browser storage writes.

[Browser results](authentication/browser-check.json).
These checks establish preview behavior only; they do not validate OAuth.

| Screen | Screenshot |
| --- | --- |
| Local recovery | [Dark](authentication/local-dark.png) |
| Hosted sign-in | [Dark](authentication/signin-dark.png) · [Light](authentication/signin-light.png) |
| Admin setup | [Desktop](authentication/setup-dark.png) · [390px](authentication/setup-390-dark.png) |


## Local implementation verification

Checked 2026-09-23 against the built local chassis in a disposable project and
config/data directories. No live provider registration or user content was used.

- Full Go suite with the race detector passed, including runtime socket tests.
- The frontend regression sweep identified three mock CSS convention failures;
  all were fixed and the affected 518-test auth/identity/style suite passed.
  The other 3,992 tests in that sweep passed; one existing test was skipped.
- Production build and all 2,299-message locale checks passed across 12 locales.
- Eleven browser layouts passed: dark/light, mobile 320/390px, OS-language
  English/French/German/Korean/Japanese, recovery, unavailable and confirmation.
- No protected API calls before session verification; reopening help grants no
  access. Confirmation Cancel restores account-menu focus. A failed sign-out
  retains access; retry revokes only that session. The previous bearer fails,
  another session remains valid, and the trusted launch link works again.
- Server shutdown drains upgraded runtime sockets before releasing the project
  descriptor, including script-authenticated sockets.

[Implementation browser results](authentication/implementation-browser-check.json).
[Recovery screen](authentication/implemented-local-dark.png).
[Mobile Korean recovery](authentication/implemented-local-ko-390.png).
[End session confirmation](authentication/implemented-end-session.png).

These checks validate local session behavior, not OIDC or hosted authorization.
