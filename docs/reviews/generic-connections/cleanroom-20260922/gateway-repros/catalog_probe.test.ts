import { readFileSync } from 'node:fs'
import { it, expect } from '../desk/web/node_modules/vitest'
import { parseConnectionCatalog } from '../desk/web/src/connections/catalog'
import { readResourcePage } from '../desk/web/src/connections/resourceProtocol'

it('Desk accepts the actual gateway catalog with all current providers supported', () => {
 const catalog = parseConnectionCatalog(JSON.parse(readFileSync(new URL('./catalog.json', import.meta.url), 'utf8')))
 expect(catalog.providers.map(provider => provider.id)).toEqual(['google-drive', 'gmail', 'notion', 'obsidian'])
 expect(catalog.unsupported).toBeUndefined()
 expect(catalog.web).toBe(true)
})
it('Desk accepts an empty resource page with continuation', () => {
 expect(readResourcePage({ selectionContext: 'epoch', items: [], more: true, nextPageToken: 'page-2' }).more).toBe(true)
})
