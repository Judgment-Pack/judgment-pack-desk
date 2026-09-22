# Desk review reproduction commands

All production source in `web/` is copied from Desk head `c67f6096c4285a62726d7c39b4615ee2a99803c6`. Its `node_modules` symlink uses the dependencies supplied with the reviewed clean-room checkout. Only the two `cleanroom-*.test.*` files are added. No changes remain in the copied production `ConnectionsPane.tsx`.

```bash
export PATH=/home/onword/.nvm/versions/node/v22.23.1/bin:$PATH
cd /tmp/jp-foundation-cleanroom-20260922/desk-repros/web

# Expected: one failing test, Continue button disabled after successful setup.
npm test -- --reporter=verbose src/connections/cleanroom-oauth-form.test.tsx

# Expected: three passing tests, no provider/catalog/network access during reverify.
npm test -- --reporter=verbose src/documents/cleanroom-resource-pin.test.ts
```

Saved initial failure: `oauth-form.log`. Saved proof-of-fix run: `oauth-form-fix-proof.log`. Suggested minimal condition patch: `oauth-form-fix-direction.patch`. It was applied only to this disposable copy for the proof and then the copied production file was restored. The patch has not been applied to the reviewed checkout.

Repository validation performed independently during the review:

```bash
cd /tmp/jp-foundation-cleanroom-20260922/desk/web
npm test -- --reporter=dot src/connections src/documents
npm run typecheck
npm run i18n:check

cd /tmp/jp-foundation-cleanroom-20260922/desk
GOCACHE=/tmp/jp-authoring-go-cache GOFLAGS=-buildvcs=false go test ./internal/desk
git diff --check
git status --short
```

The Go test command requires loopback socket permissions for `httptest`; the first sandboxed attempt failed before tests could proceed, and the approved escalated rerun passed. No external service is required by these reproduction commands.
