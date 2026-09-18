import { expect, it } from 'vitest'
import { decodeDeskConfig, DOCUMENT_DEFAULTS } from '../config/deskConfig'
it('preserves missing and explicitly disabled document settings as distinct values', () => {
  expect(decodeDeskConfig('{"deskConfigVersion":1,"research":{}}','desk').values?.research?.documents).toBeUndefined()
  expect(decodeDeskConfig('{"deskConfigVersion":1,"research":{"documents":{"source":"documents"}}}','desk').values?.research?.documents).toEqual(DOCUMENT_DEFAULTS)
  expect(decodeDeskConfig('{"deskConfigVersion":1,"research":{"documents":null}}','desk').values?.research?.documents).toBeNull()
  expect(decodeDeskConfig('{"deskConfigVersion":1,"research":{"documents":{"source":"documents"}}}','project').values).toBeUndefined()
})
