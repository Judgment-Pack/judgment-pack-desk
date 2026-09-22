import { msg } from '../i18n'
import type { ConnectionProvider } from './client'

/** Only implemented providers belong here; gateway status determines availability. */
export const CONNECTION_PROVIDERS: readonly ConnectionProvider[] = ['google-drive', 'gmail', 'notion', 'obsidian']
export function providerName(provider: ConnectionProvider): string {
 switch (provider) { case 'google-drive': return 'Google Drive'; case 'gmail': return 'Gmail'; case 'notion': return 'Notion'; case 'obsidian': return 'Obsidian' }
}
export function providerDescription(provider: ConnectionProvider): string {
 switch (provider) {
  case 'google-drive': return msg('Files, documents, spreadsheets, and presentations')
  case 'gmail': return msg('Emails and conversations')
  case 'notion': return msg('Pages and workspace knowledge')
  case 'obsidian': return msg('Notes in a local vault')
 }
}
