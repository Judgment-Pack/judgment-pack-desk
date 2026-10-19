import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'
import { LinkedMessageText } from './LinkedMessageText'
import { MessageRenderer } from './MessageRenderer'
afterEach(cleanup)

it('links repeated user-supplied domains while preserving visible text, punctuation and line breaks', () => {
 const text = 'check the judgmentpack.org.\nThen judgmentpack.org and (example.org/docs#overview).'
 const view = render(<LinkedMessageText text={text}/>)
 const links = screen.getAllByRole('link', {name:'judgmentpack.org'})
 expect(links).toHaveLength(2)
 for (const link of links) {
  expect(link.getAttribute('href')).toBe('https://judgmentpack.org/')
  expect(link.getAttribute('target')).toBe('_blank')
  expect(link.getAttribute('rel')).toBe('noopener noreferrer')
  expect(link.tabIndex).toBe(0)
 }
 expect(screen.getByRole('link',{name:'example.org/docs#overview',description:'Opens in a new tab'}).getAttribute('href')).toBe('https://example.org/docs#overview')
 const visible = view.container.cloneNode(true) as HTMLElement
 visible.querySelectorAll('[hidden]').forEach(node=>node.remove())
 expect(visible.textContent).toBe(text)
})

it('keeps user HTML literal and leaves emails, file paths and unsafe schemes unlinked', () => {
 const view = render(<LinkedMessageText text={'<script>alert(1)</script> user@example.org /tmp/example.org report.json javascript:alert(1) ftp://example.org http://example.org/help'}/>)
 expect(view.container.querySelector('script')).toBeNull()
 expect(screen.getAllByRole('link')).toHaveLength(1)
 expect(screen.getByRole('link',{name:'http://example.org/help'}).getAttribute('href')).toBe('http://example.org/help')
})

it('uses the same website component for assistant prose and user text without replacing explicit labels', () => {
 const user = render(<LinkedMessageText text="judgmentpack.org"/>)
 const className = screen.getByRole('link').className
 user.unmount()
 const view = render(<MessageRenderer text={'Read **judgmentpack.org**, www.example.org, and [the guide](https://example.org/guide#top). [bare destination](example.org/help).'}/>)
 expect(screen.getByRole('link',{name:'judgmentpack.org'}).className).toBe(className)
 expect(screen.getByRole('link',{name:'www.example.org'}).getAttribute('href')).toBe('https://www.example.org/')
 expect(screen.getByRole('link',{name:'the guide'}).getAttribute('href')).toBe('https://example.org/guide#top')
 expect(screen.getByRole('link',{name:'bare destination'}).getAttribute('href')).toBe('https://example.org/help')
 expect(view.container.querySelector('a a')).toBeNull()
})

it('does not link code or turn citation labels, images and unsafe destinations into website navigation', () => {
 const view = render(<MessageRenderer text={'`example.org`\n\n```json\n{"url":"https://example.org"}\n```\n\n![example.org](https://example.org/image.png) [example.org](javascript:alert(1)) [www.example.org](http://example.org/help)'}/>)
 expect(screen.getAllByRole('link')).toHaveLength(1)
 expect(screen.getByRole('link',{name:'www.example.org'}).getAttribute('href')).toBe('http://example.org/help')
 expect(view.container.querySelectorAll('code a, pre a, img, script')).toHaveLength(0)
})

it('does not make relative paths, credentials or control-character URLs clickable', () => {
 render(<MessageRenderer text={'[local](/settings) [credentials](https://user:pass@example.org/) [data](data:text/html,hello) [broken](https://) [email](mailto:user@example.org)'}/>)
 expect(screen.queryByRole('link')).toBeNull()
})
