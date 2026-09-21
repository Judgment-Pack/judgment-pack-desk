import notion from './assets/notion.svg'
import obsidian from './assets/obsidian.svg'
import type { SourceProvider } from './client'
export function ProviderIcon({ provider }: { provider: SourceProvider }) {
 return <img src={provider === 'notion' ? notion : obsidian} width={16} height={16} alt="" aria-hidden="true" />
}
