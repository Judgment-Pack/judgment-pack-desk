import { msg } from '../i18n'
import { localized, type ConnectionDescriptor } from './catalog'
import type { ConnectionProvider } from './client'

/** Display copy stays local; the gateway catalog supplies provider availability. */
export function providerName(provider: ConnectionProvider, descriptor?: ConnectionDescriptor): string {
 if (descriptor?.presentation) return descriptor.presentation.name
 switch (provider) { case 'google-drive': return 'Google Drive'; case 'gmail': return 'Gmail'; case 'notion': return 'Notion'; case 'obsidian': return 'Obsidian'; default: return provider }
}
export function providerDescription(provider: ConnectionProvider, descriptor?: ConnectionDescriptor): string {
 if (descriptor?.presentation) return localized(descriptor.presentation.description)
 switch (provider) {
  case 'google-drive': return msg('Files, documents, spreadsheets, and presentations')
  case 'gmail': return msg('Emails and conversations')
  case 'notion': return msg('Pages and workspace knowledge')
  case 'obsidian': return msg('Notes in a local vault')
  default: return ''
 }
}
