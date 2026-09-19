import { expect, it } from 'vitest'
import { googleRegistration } from './registration'

it('accepts Desktop identity while dropping endpoint overrides and unrelated fields', () => {
  expect(googleRegistration(JSON.stringify({ installed: { client_id: 'test.apps.googleusercontent.com', client_secret: 'synthetic', token_uri: 'https://wrong.invalid', redirect_uris: ['https://wrong.invalid'] }, unrelated: 'private' })))
    .toEqual({ clientId: 'test.apps.googleusercontent.com', clientSecret: 'synthetic' })
  expect(googleRegistration('{"installed":{"client_id":"test.apps.googleusercontent.com"}}')).toEqual({ clientId: 'test.apps.googleusercontent.com', clientSecret: '' })
})

it.each([
  'null', '[]', 'true', '{}', '{"web":{"client_id":"test.apps.googleusercontent.com"}}',
  '{"installed":{"client_id":"test.apps.googleusercontent.com"},"type":"service_account"}',
  '{"installed":{"client_id":"test.apps.googleusercontent.com"},"web":{}}',
  '{"installed":{"client_id":"test.apps.googleusercontent.com","client_id":"other.apps.googleusercontent.com"}}',
  '{"installed":{"client_id":"test.apps.googleusercontent.com","client_secret":null}}',
  '{"installed":{"client_id":"test.apps.googleusercontent.com","client_secret":"\\ud800"}}',
  ...['wrong', '\n'].map(client_id => JSON.stringify({ installed: { client_id } })),
  ...[123, 'line\nbreak', 'ü'.repeat(2049), 'a'.repeat(16_385)].map(client_secret => JSON.stringify({ installed: { client_id: 'test.apps.googleusercontent.com', client_secret } })),
])('refuses invalid registration without exposing the input (%#)', text => {
  expect(() => googleRegistration(text)).toThrow(SyntaxError)
  try { googleRegistration(text) } catch (error) { expect((error as Error).message).toBe('') }
})
