import { useState } from 'react'
import styles from './BrandMark.module.css'

/** Same asset as judgmentpack.org's header, bundled for offline desks. */
export const DEFAULT_DESK_LOGO = '/favicon.svg'

/** Render custom SVG as an image resource, never as executable markup. */
export function markToDataUri(mark: string | null): string | undefined {
  if (mark === null) return undefined
  const trimmed = mark.trim()
  if (trimmed.startsWith('data:image/')) return trimmed
  if (trimmed.startsWith('<svg')) return `data:image/svg+xml,${encodeURIComponent(trimmed)}`
  return undefined
}

export function BrandMark({ mark = null, className }: { mark?: string | null; className?: string }) {
  const src = markToDataUri(mark) ?? DEFAULT_DESK_LOGO
  const [failed, setFailed] = useState<string>()
  return <img className={[styles.mark, className].filter(Boolean).join(' ')} src={failed === src ? DEFAULT_DESK_LOGO : src}
    width={24} height={24} alt="" aria-hidden="true" onError={() => setFailed(src)} />
}
