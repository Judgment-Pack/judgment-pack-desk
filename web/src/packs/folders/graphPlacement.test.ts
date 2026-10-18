import {expect,it} from 'vitest'
import {applyFolderAction as apply,decodeFolders,defaultFolders,HOME_FOLDER,packFolder} from './model'
import {projectGraphPlacements} from './graphPlacement'
const first={originChatId:'origin-one',draftId:'same-draft',id:'flow'}
const second={...first,originChatId:'origin-two'}
const folders=()=>apply(apply(defaultFolders(),{type:'create',id:'one',name:'One',parentId:null}),{type:'create',id:'two',name:'Two',parentId:null})
it('stores distinct scoped graph placements in explicit v3 without broadening v2',()=>{
 const one=apply(folders(),{type:'assign',packId:'graph-draft:origin-one:same-draft',folderId:'one'})
 const both=apply(one,{type:'assign',packId:'graph-draft:origin-two:same-draft',folderId:'two'})
 expect(both.version).toBe(3);expect(decodeFolders(JSON.parse(JSON.stringify(both)))).toEqual(both)
 expect(()=>decodeFolders({...both,version:2})).toThrow()
 for(const key of ['graph-draft::same-draft','graph-draft:origin-one:','graph:origin-one:same-draft','graph-draft:one:two:three'])expect(()=>decodeFolders({...both,assignments:{[key]:'one'}})).toThrow()
 const projected=projectGraphPlacements(both,[first,second]).document
 expect(packFolder(projected,'graph-draft:origin-one:same-draft')).toBe('one')
 expect(packFolder(projected,'graph-draft:origin-two:same-draft')).toBe('two')
 expect(projectGraphPlacements(both,[second],['origin-one:same-draft']).document.assignments).toEqual({'graph-draft:origin-two:same-draft':'two'})
 expect(one.assignments).toEqual({'graph-draft:origin-one:same-draft':'one'})
})
it('projects an old placement only for an unambiguous origin and retains ambiguous metadata',()=>{
 const legacy=apply(folders(),{type:'assign',packId:'graph-draft:same-draft',folderId:'one'})
 expect(legacy.version).toBe(2)
 expect(projectGraphPlacements(legacy,[first])).toEqual({document:{...legacy,version:3,assignments:{'graph-draft:origin-one:same-draft':'one'}},ambiguous:[]})
 const ambiguous=projectGraphPlacements(legacy,[first,second])
 expect(ambiguous.document.assignments).toEqual(legacy.assignments);expect(ambiguous.ambiguous).toEqual(['graph-draft:same-draft'])
 for(const draft of [first,second])expect(packFolder(ambiguous.document,'graph-draft:'+draft.originChatId+':'+draft.draftId)).toBe(HOME_FOLDER)
 // Deleting one origin must not silently assign its old placement to the other.
 expect(projectGraphPlacements(legacy,[second],['origin-one:same-draft']).ambiguous).toEqual(['graph-draft:same-draft'])
 const resolved=apply(apply(ambiguous.document,{type:'assign',packId:'graph-draft:origin-one:same-draft',folderId:'one'}),{type:'assign',packId:'graph-draft:origin-two:same-draft',folderId:'two'})
 expect(projectGraphPlacements(resolved,[first,second]).document.assignments).toEqual({'graph-draft:origin-one:same-draft':'one','graph-draft:origin-two:same-draft':'two'})
 expect(legacy.assignments).toEqual({'graph-draft:same-draft':'one'})
})
it('carries a saved draft placement into the graph without overriding an explicit choice',()=>{
 const assigned=apply(folders(),{type:'assign',packId:'graph-draft:origin-one:same-draft',folderId:'one'})
 expect(projectGraphPlacements(assigned,[{...first,saved:true}]).document.assignments['graph:flow']).toBe('one')
 const explicit=apply(assigned,{type:'assign',packId:'graph:flow',folderId:'two'})
 expect(projectGraphPlacements(explicit,[{...first,saved:true}]).document.assignments['graph:flow']).toBe('two')
 const conflicting=apply(assigned,{type:'assign',packId:'graph-draft:origin-two:same-draft',folderId:'two'})
 expect(projectGraphPlacements(conflicting,[{...first,saved:true},{...second,saved:true}]).document.assignments).not.toHaveProperty('graph:flow')
})
