import { memo, type ReactNode } from 'react'
import { ExternalLink } from '../ui/ExternalLink'
import { websiteLinks } from './websiteLinks'

/** User messages stay literal text: no Markdown or HTML interpretation. */
export const LinkedMessageText = memo(function LinkedMessageText({text}: {text: string}) {
 const parts: ReactNode[] = []
 let offset = 0
 for (const link of websiteLinks(text)) {
  if (link.start > offset) parts.push(text.slice(offset, link.start))
  parts.push(<ExternalLink key={link.start} href={link.href}>{link.text}</ExternalLink>)
  offset = link.end
 }
 if (offset < text.length) parts.push(text.slice(offset))
 return <>{parts}</>
})
