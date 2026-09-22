import notion from './assets/notion.svg'
import obsidian from './assets/obsidian.svg'
import { IconGoogleDrive, IconMail, IconLink } from '../shell/icons'
import type { ConnectionDescriptor } from './catalog'
import type { ConnectionProvider } from './client'
export function ProviderIcon({ provider, descriptor }: { provider: ConnectionProvider; descriptor?: ConnectionDescriptor }) {
 if (descriptor?.presentation?.icon) return <img src={descriptor.presentation.icon} width={16} height={16} alt="" aria-hidden="true" />
 if (provider === 'google-drive') return <IconGoogleDrive />
 if (provider === 'gmail') return <IconMail />
 if (provider !== 'notion' && provider !== 'obsidian') return <IconLink />
 return <img src={provider === 'notion' ? notion : obsidian} width={16} height={16} alt="" aria-hidden="true" />
}
