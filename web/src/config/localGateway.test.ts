import { describe, expect, it } from 'vitest'
import { decodeDeskConfig, DESK_DEFAULTS, DOCUMENT_DEFAULTS, effectiveConfig, withLocalGateway, type LocalGatewayStatus } from './deskConfig'

const pin = { url: 'http://127.0.0.1:12345', authority: 'gateway:desk-local', signer: { algorithm: 'ed25519' as const, public: 'ab'.repeat(32) } }
const local: LocalGatewayStatus = { status: 'ready', gateway: pin }
describe('managed connection layering', () => {
  it('enables local document processing without changing the declared configuration', () => {
    const read = { path: '/private/desk.json', present: false, sha256: '', localGateway: local }
    const effective = effectiveConfig(undefined, undefined, undefined, read)
    expect(effective.config.research.gateway).toEqual(pin)
    expect(effective.config.research.documents).toEqual(DOCUMENT_DEFAULTS)
    expect(effective.desk?.decoded).toBeUndefined()
    expect(effective.desk?.text).toBeUndefined()
    expect(effective.desk?.sha256).toBe('')
  })
  it.each([null, { ...DOCUMENT_DEFAULTS, enabled: false }])('preserves explicit disabled processing: %j', documents => {
    expect(withLocalGateway({ ...DESK_DEFAULTS.research, documents }, local).documents?.enabled ?? false).toBe(false)
  })
  it('does not turn unavailable components into a usable connection', () => {
    expect(withLocalGateway(DESK_DEFAULTS.research, { status: 'unavailable' }).gateway).toBeNull()
  })
  it('leaves external gateways and their custom sources and limits unchanged', () => {
    const external = { ...DESK_DEFAULTS.research, gateway: { ...pin, authority: 'gateway:organization' }, documents: { ...DOCUMENT_DEFAULTS, source: 'organization-pdfs', maxRequestBytes: 64 << 20 } }
    expect(withLocalGateway(external, local)).toBe(external)
  })
  it('applies installed adapter limits and does not advertise uninstalled research providers', () => {
    const declared = { ...DESK_DEFAULTS.research, documents: { ...DOCUMENT_DEFAULTS, source: 'old-external', maxRequestBytes: 64 << 20, maxResponseBytes: 16 << 20 } }
    const actual = withLocalGateway(declared, local)
    expect(actual.documents).toEqual({ ...DOCUMENT_DEFAULTS })
    expect(actual.sources).toEqual({ search: null, read: null, web: null })
    expect(declared.documents.source).toBe('old-external')
  })
  it('marks the layered gateway as the managed local one, and an external one as not', () => {
    expect(withLocalGateway(DESK_DEFAULTS.research, local).managedLocal).toBe(true)
    const external = { ...DESK_DEFAULTS.research, gateway: { ...pin, authority: 'gateway:organization' }, sources: { search: null, read: null, web: { source: 'web' as const } } }
    expect(withLocalGateway(external, local).managedLocal).toBeUndefined()
    expect(withLocalGateway(external, local).sources.web).toEqual({ source: 'web' })
    expect(DESK_DEFAULTS.research.managedLocal).toBeUndefined()
  })
  it('never overlays a refused personal configuration', () => {
    const effective = effectiveConfig(undefined, undefined, undefined, { path: '/private/desk.json', present: true, decoded: decodeDeskConfig('{broken', 'desk'), localGateway: local })
    expect(effective.config.research.gateway).toBeNull()
  })
})
