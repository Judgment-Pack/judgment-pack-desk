import { useId, type ReactNode } from 'react'
import { msg, useLocale } from '../i18n'
import { MAX_LINK_BYTES, normalizeLink } from '../documents/link'
import styles from './ExternalLink.module.css'

/** Navigation allows HTTP as well as HTTPS. It grants no source-reading access. */
export function externalHref(value: unknown): string | undefined {
 if (typeof value !== 'string' || new TextEncoder().encode(value).length > MAX_LINK_BYTES || /[\\\x00-\x20\x7f]/.test(value)) return
 if (/^https?:\/\//i.test(value)) {
  try {
   const url = new URL(value)
   if (url.username || url.password || value.split('://')[1]?.split(/[/?#]/, 1)[0]?.includes('@')) return
   return url.href
  } catch { return }
 }
 const link = normalizeLink(value)
 if (!('refused' in link)) return link.fetchUrl + (link.anchor ? `#${link.anchor}` : '')
}

/** Inline website navigation shares tokens, keyboard treatment and new-tab semantics. */
export function ExternalLink({ href, children }: { href?: string; children: ReactNode }) {
 useLocale()
 const description = useId()
 const target = externalHref(href)
 if (!target) return <span>{children}</span>
 return <><a className={styles.link} href={target} target="_blank" rel="noopener noreferrer" aria-describedby={description}>{children}</a><span hidden id={description}>{msg('Opens in a new tab')}</span></>
}
