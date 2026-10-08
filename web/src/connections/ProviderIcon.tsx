import notion from './assets/notion.svg'
import obsidian from './assets/obsidian.svg'
import googleDrive from './assets/google-drive.svg'
import gmail from './assets/gmail.svg'
import amazonS3 from './assets/amazon-s3.svg'
import { IconLink } from '../shell/icons'
import type { ConnectionDescriptor } from './catalog'
import type { ConnectionProvider } from './client'
// Known services use bundled official marks, including before their catalog loads.
const brandMarks = new Map([['google-drive', googleDrive], ['gmail', gmail], ['aws-s3', amazonS3]])

export function ProviderIcon({ provider, descriptor }: { provider: ConnectionProvider; descriptor?: ConnectionDescriptor }) {
 const mark = brandMarks.get(provider) ?? descriptor?.presentation?.icon
 if (mark) return <img src={mark} width={16} height={16} alt="" aria-hidden="true" />
 if (provider !== 'notion' && provider !== 'obsidian') return <IconLink />
 return <img src={provider === 'notion' ? notion : obsidian} width={16} height={16} alt="" aria-hidden="true" />
}
