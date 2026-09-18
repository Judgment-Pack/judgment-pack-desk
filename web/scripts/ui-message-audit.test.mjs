import { test } from 'node:test'
import assert from 'node:assert/strict'
import { untranslatedUiMessages as audit } from './ui-message-audit.mjs'
test('finds component labels, nested options, confirmations and mixed text', () => {
  const source = '<><Select options={[{ value: "draft", label: "Draft" }]} /><Map ariaLabel="Pack map" /><p>Saved</p><p>{"Try again"}</p></>; window.confirm("Leave? Changes will be lost.")'
  assert.deepEqual(audit(source).map(x => x.text), ['Draft', 'Pack map', 'Saved', 'Try again', 'Leave? Changes will be lost.'])
})
test('preserves protocol values, user data, identifiers, translated messages and control flow', () => {
  assert.deepEqual(audit('<><Select options={[{value: "draft", label: msg("Draft")}]} /><p>{file.name}</p><p>{mode === "draft" ? msg("Draft") : title}</p><code>sha256</code><Message text="Use <0/>" /></>'), [])
})

test('checks loading copy and page metadata as well as ordinary labels', () => {
  assert.deepEqual(audit('<><Loading what={`pack ${id}`} /><Header meta="model unavailable" /></>').map(x => x.text), ['pack {{value}}', 'model unavailable'])
})

test('checks literal captions inside translated slots without treating props or protocol fields as copy', () => {
  assert.deepEqual(audit('<Message text="Context: <0/>" slots={[pack.id ?? "Current draft"]} />').map(x => x.text), ['Current draft'])
  assert.deepEqual(audit('<Message text="<0/>" slots={[<Button variant="quiet">{msg("Save")}</Button>, <code>op</code>, phase === "idle" ? msg("Not started") : phase]} />'), [])
})
