import { expect,it } from 'vitest'
import { searchItems,switchHref, type SwitchItem } from './quickSwitch'
const items:SwitchItem[]=[{id:'p',title:'Travel',kind:'Pack',detail:'Expenses Finance',href:'/packs/travel'},{id:'d',title:'Finance',kind:'Folder',detail:'Company / Finance',href:'/library?folder=finance'}]
it('matches multiple terms and ranks recent destinations without inserting stale/deleted items',()=>{
 expect(searchItems(items,'finance pack',[]).map(x=>x.id)).toEqual(['p'])
 expect(searchItems(items,'',['/deleted',items[0]!.href]).map(x=>x.id)).toEqual(['p','d'])
 expect(searchItems(items,'missing',[])).toEqual([])
})
it('keeps test-tab visits with their pack and excludes sensitive or unrelated URLs',()=>{
 expect(switchHref('/packs/travel/evaluate','?chat=private')).toBe('/packs/travel')
 expect(switchHref('/library','?folder=finance')).toBe('/library?folder=finance')
 expect(switchHref('/launch','?secret=private')).toBeUndefined()
 expect(switchHref('/packs/new','')).toBeUndefined()
 expect(switchHref('/dialects/prep-one','?chat=private&tab=recipe')).toBe('/dialects/prep-one')
 expect(switchHref('/dialects/drafts/draft-one','?chat=private')).toBe('/dialects/drafts/draft-one')
 expect(switchHref('/policies/policy-one','?chat=private')).toBe('/policies/policy-one')
 expect(switchHref('/policies/new','')).toBeUndefined()
 expect(switchHref('/dialects/new','')).toBeUndefined()
 expect(switchHref('/dialects/templates','')).toBeUndefined()
 expect(switchHref('/library','?folder=finance&type=dialect&chat=private')).toBe('/library?folder=finance&type=dialect')
})
