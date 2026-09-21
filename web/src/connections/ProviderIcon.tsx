import notion from './assets/notion.svg'
import obsidian from './assets/obsidian.svg'
import { IconGoogleDrive, IconMail } from '../shell/icons'
import type { ConnectionProvider } from './client'
export function ProviderIcon({ provider }: { provider: ConnectionProvider }) {
 if (provider === 'google-drive') return <IconGoogleDrive />
 if (provider === 'gmail') return <IconMail />
 return <img src={provider === 'notion' ? notion : obsidian} width={16} height={16} alt="" aria-hidden="true" />
}
