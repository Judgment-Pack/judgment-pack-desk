import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { forgetSession, giveThisPageASessionForTesting } from '../../mcp/session'
import { sourceReviewClient } from './client'

const reviewId = '0123456789abcdef0123456789abcdef', approvalId = 'abcdef0123456789abcdef0123456789'
const hash = 'a'.repeat(64), at = '2026-01-02T03:04:05Z'
const approve = {reviewId, reviewRevision: 1 as const}, revoke = {approvalId, approvalRevision: 1 as const}
// Complete handwritten catalog; no fixture captured from a server or model run.
function catalog(approved = false) {
  return {version: 1, sources: [
    ['desk-search', 'search_sources', 'discover', 'read-external', 'read', 'research', 'none'],
    ['desk-link', 'read_link', 'read', 'read-external', 'read', 'read', 'none'],
    ['desk-website', 'explore_website', 'discover', 'read-external', 'read', 'research', 'none'],
    ['desk-research-workflow', 'request_dialect_research', 'act', 'act', 'act', '', 'required'],
  ].map(([adapterId, name, operation, effect, effectClass, delegation, approval], index) => ({
    snapshot: {adapterId, adapterVersion: '1', kind: 'custom', trust: 'first-party', configurationDigest: hash, toolListDigest: hash,
      completeDeclarations: [{name, description: 'Complete fixture declaration.', inputSchema: {type: 'object', properties: {}, additionalProperties: false}, taxonomy: {operation, effect}, effectClass, delegation, approval}],
      configuration: {target: index === 0 ? 'https://source.example.test' : '', targetDigest: index === 0 ? hash : '', authorityDigest: index === 0 ? hash : '', publicKeyDigest: index === 0 ? hash : '', deskDigest: hash, preferenceDigest: index === 0 ? 'absent' : '', connection: index === 0 ? 'search' : '', revision: index === 0 ? '1' : '', provider: index === 0 ? 'fixture' : '', bundleDigest: '', verifierDigest: '', gatewayBuildDigest: '', ownerDigest: hash, catalogDigest: '', catalogOwnerDigest: ''},
      status: index === 0 ? 'review-required' : 'setup-required', ...(index === 0 ? {identity: {kind: 'connection', label: 'Desk research HTTP ' + hash}, identityDigest: hash} : {})},
    status: index === 0 ? approved ? 'approved' : 'review-required' : 'setup-required',
    ...(index === 0 ? approved ? {approval: {id: approvalId, revision: 1, approvedAt: at}} : {reviewId, reviewRevision: 1} : {}),
  }))}
}
const revocation = () => ({version: 1, status: 'revoked', approval: {id: approvalId, revision: 2, approvedAt: at, revokedAt: '2026-01-02T03:05:05Z'}})
const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), {status, headers: {'Content-Type': 'application/json'}})
beforeEach(() => giveThisPageASessionForTesting('source-review-session'))
afterEach(() => { vi.unstubAllGlobals() })

it('uses explicit authenticated catalog, approve and revoke requests with exact selectors only', async () => {
  const fetch = vi.fn().mockResolvedValueOnce(json(catalog())).mockResolvedValueOnce(json(catalog(true))).mockResolvedValueOnce(json(revocation()))
  vi.stubGlobal('fetch', fetch)
  const client = await sourceReviewClient('chat-one')
  expect(fetch).not.toHaveBeenCalled()
  await client.review(); await client.approve(approve); await client.revoke(revoke)
  expect(fetch.mock.calls.map(([url]) => url)).toEqual(['/api/assistant-source-approvals/chat-one', '/api/assistant-source-approvals/chat-one/approve', '/api/assistant-source-approvals/chat-one/revoke'])
  for (const [, options] of fetch.mock.calls) expect(options).toMatchObject({credentials: 'omit', redirect: 'error', cache: 'no-store', headers: {Authorization: 'Bearer source-review-session'}})
  expect(fetch.mock.calls[0][1].body).toBeUndefined()
  expect(JSON.parse(fetch.mock.calls[1][1].body)).toEqual(approve)
  expect(JSON.parse(fetch.mock.calls[2][1].body)).toEqual(revoke)
  await expect(client.revoke(revoke)).rejects.toMatchObject({delivery: 'not-sent'})
  expect(fetch).toHaveBeenCalledTimes(3)
})
it('refuses missing observation, path injection, foreign selectors and client-added authority without sending', async () => {
  const fetch = vi.fn().mockResolvedValue(json(catalog())); vi.stubGlobal('fetch', fetch)
  await expect(sourceReviewClient('../other')).rejects.toMatchObject({delivery: 'not-sent'})
  const client = await sourceReviewClient('chat-one')
  await expect(client.approve(approve)).rejects.toMatchObject({delivery: 'not-sent'})
  await client.review()
  await expect(client.approve({...approve, reviewId: 'f'.repeat(32)})).rejects.toMatchObject({delivery: 'not-sent'})
  await expect(client.approve({...approve, scope: {}} as typeof approve)).rejects.toMatchObject({delivery: 'not-sent'})
  await expect(client.revoke(revoke)).rejects.toMatchObject({delivery: 'not-sent'})
  expect(fetch).toHaveBeenCalledTimes(1)
})
it.each([409, 403, 413, 500, 408])('retains refusal or uncertainty for HTTP %s and requires explicit review before another decision', async status => {
  const fetch = vi.fn().mockResolvedValueOnce(json(catalog())).mockResolvedValueOnce(json({error: 'private upstream text'}, status)); vi.stubGlobal('fetch', fetch)
  const client = await sourceReviewClient('chat-one'); await client.review()
  await expect(client.approve(approve)).rejects.toMatchObject({delivery: status < 500 && status !== 408 ? 'refused' : 'uncertain', status})
  await expect(client.approve(approve)).rejects.toMatchObject({delivery: 'not-sent'})
  expect(fetch).toHaveBeenCalledTimes(2)
})
it('never retries a lost mutation acknowledgement; explicit observation can discover the saved approval', async () => {
  const fetch = vi.fn().mockResolvedValueOnce(json(catalog())).mockRejectedValueOnce(Error('private upstream text')).mockResolvedValueOnce(json(catalog(true))); vi.stubGlobal('fetch', fetch)
  const client = await sourceReviewClient('chat-one'); await client.review()
  await expect(client.approve(approve)).rejects.toMatchObject({delivery: 'uncertain', failure: {outcome: 'unknown'}})
  await expect(client.approve(approve)).rejects.toMatchObject({delivery: 'not-sent'})
  expect(fetch).toHaveBeenCalledTimes(2)
  expect((await client.review()).sources[0].approval?.id).toBe(approvalId)
  await expect(client.approve(approve)).rejects.toMatchObject({delivery: 'not-sent'})
  expect(fetch.mock.calls.filter(([, options]) => options.method === 'POST')).toHaveLength(1)
})
it('serializes duplicate decisions while the first request is in flight', async () => {
  let complete!: (value: Response) => void
  const wait = new Promise<Response>(resolve => { complete = resolve })
  const fetch = vi.fn().mockResolvedValueOnce(json(catalog())).mockReturnValueOnce(wait); vi.stubGlobal('fetch', fetch)
  const client = await sourceReviewClient('chat-one'); await client.review()
  const first = client.approve(approve)
  await expect(client.approve(approve)).rejects.toMatchObject({delivery: 'not-sent'})
  await expect(client.review()).rejects.toMatchObject({delivery: 'not-sent'})
  complete(json(catalog(true))); await first
  expect(fetch).toHaveBeenCalledTimes(2)
})
it('refuses a replacement session before sending and discards responses after session end', async () => {
  const fetch = vi.fn().mockResolvedValue(json(catalog())); vi.stubGlobal('fetch', fetch)
  const old = await sourceReviewClient('chat-one'); forgetSession(); giveThisPageASessionForTesting('replacement')
  await expect(old.review()).rejects.toMatchObject({delivery: 'not-sent'})
  expect(fetch).not.toHaveBeenCalled()
  const client = await sourceReviewClient('chat-one')
  fetch.mockImplementationOnce(async () => { forgetSession(); return json(catalog()) })
  await expect(client.review()).rejects.toMatchObject({delivery: 'uncertain'})
  expect(fetch).toHaveBeenCalledTimes(1)
})
it('aborted observation cannot expose approval controls', async () => {
  const fetch = vi.fn(); vi.stubGlobal('fetch', fetch)
  const client = await sourceReviewClient('chat-one'), signal = new AbortController(); signal.abort()
  await expect(client.review(signal.signal)).rejects.toMatchObject({delivery: 'not-sent'})
  await expect(client.approve(approve)).rejects.toMatchObject({delivery: 'not-sent'})
  expect(fetch).not.toHaveBeenCalled()
})
it.each(['malformed', 'changed-snapshot', 'not-approved'])('treats %s approval acknowledgement as uncertain, without resending', async kind => {
  const changed = catalog(true)
  if (kind === 'changed-snapshot') changed.sources[0].snapshot.configuration.provider = 'changed-provider'
  const response = kind === 'malformed' ? {} : kind === 'not-approved' ? catalog() : changed
  const fetch = vi.fn().mockResolvedValueOnce(json(catalog())).mockResolvedValueOnce(json(response)); vi.stubGlobal('fetch', fetch)
  const client = await sourceReviewClient('chat-one'); await client.review()
  await expect(client.approve(approve)).rejects.toMatchObject({delivery: 'uncertain', failure: {code: 'invalid-response'}})
  await expect(client.approve(approve)).rejects.toMatchObject({delivery: 'not-sent'})
  expect(fetch).toHaveBeenCalledTimes(2)
})
it.each(['foreign-id', 'changed-approved-at', 'not-revoked'])('refuses a %s revocation acknowledgement', async kind => {
  const response = revocation()
  if (kind === 'foreign-id') response.approval.id = 'f'.repeat(32)
  if (kind === 'changed-approved-at') response.approval.approvedAt = '2026-01-01T00:00:00Z'
  if (kind === 'not-revoked') response.approval.revision = 1
  const fetch = vi.fn().mockResolvedValueOnce(json(catalog(true))).mockResolvedValueOnce(json(response)); vi.stubGlobal('fetch', fetch)
  const client = await sourceReviewClient('chat-one'); await client.review()
  await expect(client.revoke(revoke)).rejects.toMatchObject({delivery: 'uncertain', failure: {code: 'invalid-response'}})
  await expect(client.revoke(revoke)).rejects.toMatchObject({delivery: 'not-sent'})
  expect(fetch).toHaveBeenCalledTimes(2)
})
it.each(['content-type', 'invalid-utf8', 'oversized'])('refuses %s response bytes and leaves review unavailable', async kind => {
  const response = kind === 'content-type' ? new Response(JSON.stringify(catalog())) : new Response(kind === 'invalid-utf8' ? new Uint8Array([0xff]) : ' '.repeat(16 * 1024 * 1024 + 1), {headers: {'Content-Type': 'application/json'}})
  const fetch = vi.fn().mockResolvedValue(response); vi.stubGlobal('fetch', fetch)
  const client = await sourceReviewClient('chat-one')
  await expect(client.review()).rejects.toMatchObject({delivery: 'uncertain'})
  await expect(client.approve(approve)).rejects.toMatchObject({delivery: 'not-sent'})
  expect(fetch).toHaveBeenCalledTimes(1)
})
