import { expect, it } from 'vitest'
import { decodeDeskConfig, DOCUMENT_DEFAULTS } from '../config/deskConfig'
it('defaults only an explicitly enabled documents source and preserves local-only desks', () => {
  expect(decodeDeskConfig('{"deskConfigVersion":1,"research":{}}','desk').values?.research?.documents).toBeUndefined()
  expect(decodeDeskConfig('{"deskConfigVersion":1,"research":{"documents":{"source":"documents"}}}','desk').values?.research?.documents).toEqual(DOCUMENT_DEFAULTS)
  expect(decodeDeskConfig('{"deskConfigVersion":1,"research":{"documents":null}}','desk').values?.research?.documents).toBeUndefined()
  expect(decodeDeskConfig('{"deskConfigVersion":1,"research":{"documents":{"source":"documents"}}}','project').values).toBeUndefined()
})
