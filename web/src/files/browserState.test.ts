import { afterEach, expect, it } from 'vitest'
import { fileTree, parents, readFileBrowser, writeFileBrowser } from './browserState'
afterEach(()=>localStorage.clear())
it('builds directories from real paths, with folders first and numeric name order',()=>{
 const tree=fileTree(['z.json','packs/item10.json','packs/item2.json','.desk/job-drafts/a.json','audit/evaluations.jsonl'].map(path=>({path,bytes:0,sha256:''})))
 expect(tree.map(node=>node.name)).toEqual(['.desk','audit','packs','z.json'])
 expect(tree[2]!.children.map(node=>node.name)).toEqual(['item2.json','item10.json'])
 expect(tree[0]!.children[0]!.children[0]!.path).toBe('.desk/job-drafts/a.json')
 expect(parents('.desk/job-drafts/a.json')).toEqual(['.desk','.desk/job-drafts'])
})
it('keeps browser preferences per desk without persisting file content',()=>{
 const value={selected:'packs/a.json',expanded:['packs'],search:'a',scroll:121,collapsed:true,wrap:true}
 writeFileBrowser('/desk-a',value)
 expect(readFileBrowser('/desk-a')).toEqual(value)
 expect(readFileBrowser('/desk-b')).toMatchObject({search:'',scroll:0,collapsed:false})
 expect(readFileBrowser('/desk-b').selected).toBeUndefined()
})
it('tolerates malformed browser storage and validates saved types',()=>{
 localStorage.setItem('jpack.files-browser.v1:/a','not json')
 expect(readFileBrowser('/a').expanded).toBeUndefined()
 localStorage.setItem('jpack.files-browser.v1:/a',JSON.stringify({selected:12,search:{},expanded:[true,'packs'],scroll:-44,collapsed:'true'}))
 expect(readFileBrowser('/a')).toMatchObject({selected:undefined,search:'',expanded:['packs'],scroll:0,collapsed:false})
})
