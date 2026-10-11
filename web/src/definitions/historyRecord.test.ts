import { expect, it } from 'vitest'
import { historyRecordField } from './historyRecord'
it('displays original input numbers and escapes without a JSON round trip', () => {
  const input='{"n":9007199254740993,"n2":1.00,"label":"\\u0061"}'
  expect(historyRecordField('{"inputs":'+input+',"at":"today"}','inputs')).toBe(input)
  expect(historyRecordField('{"inputs":{},"inputs":{"n":1}}','inputs')).toBeUndefined()
  expect(historyRecordField('{"inputs":{}}','cites')).toBeUndefined()
})
