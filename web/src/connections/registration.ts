import { parseJsonText } from '../research/verify/canon'

/** Import app identity only. Provider endpoints and account credentials never
 * become configurable through a downloaded registration. */
export function googleRegistration(text: string): { clientId: string; clientSecret: string } {
  try {
    if (new TextEncoder().encode(text).length > 16_384) throw new Error()
    parseJsonText(text)
    const root = JSON.parse(text)
    if (!root || Array.isArray(root) || 'web' in root || 'type' in root) throw new Error()
    const client = root.installed
    const { client_secret: secret = '' } = client ?? {}
    if (!client || typeof client.client_id !== 'string' || !/^[A-Za-z0-9_-]{1,220}\.apps\.googleusercontent\.com$/.test(client.client_id)
      || typeof secret !== 'string' || new TextEncoder().encode(secret).length > 4096 || /[\r\n\0]/.test(secret)) throw new Error()
    return { clientId: client.client_id, clientSecret: secret }
  } catch {
    // Never echo a parser error or any part of the imported document.
    throw new SyntaxError()
  }
}
