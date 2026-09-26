import { describe,expect,it } from 'vitest'
import { applyFolderAction as apply,defaultFolders,decodeFolders,folderPath,folderName,packFolder,inFolder,HOME_FOLDER,MAX_DEPTH } from './model'
const create=(id:string,parentId:string|null=HOME_FOLDER,name=id)=>({type:'create' as const,id,parentId,name})
describe('project folder organization',()=>{
 it('retains empty nested folders and home fallback, without changing a pack ID or file',()=>{
  const empty=defaultFolders(), nested=apply(apply(empty,create('operations')),create('intake','operations'))
  expect(decodeFolders(JSON.parse(JSON.stringify(nested)))).toEqual(nested)
  expect(packFolder(nested,'old-pack')).toBe(HOME_FOLDER)
  const moved=apply(nested,{type:'assign',packId:'old-pack',folderId:'intake'})
  const renamed=apply(moved,{type:'rename',id:'operations',name:'Customer success'})
  const relocated=apply(renamed,{type:'move',id:'operations',parentId:null})
  expect(folderPath(relocated,'intake')).toBe('Customer success / intake')
  expect(packFolder(relocated,'old-pack')).toBe('intake')
  expect(Object.keys(relocated.assignments)).toEqual(['old-pack'])
  expect(empty).toEqual(defaultFolders())
 })
 it('disallows cycles, missing parents, duplicate sibling names and renaming home',()=>{
  const doc=apply(apply(defaultFolders(),create('a',null,'Operations')),create('b','a'))
  expect(()=>apply(doc,{type:'move',id:'a',parentId:'b'})).toThrow(/themselves/)
  expect(()=>apply(doc,create('c','missing'))).toThrow(/no longer exists/)
  expect(()=>apply(doc,create('c',null,'OPERATIONS'))).toThrow(/already exists/)
  expect(()=>apply(doc,{type:'rename',id:HOME_FOLDER,name:'me'})).toThrow(/home folder/)
  expect(()=>apply(doc,{type:'delete',id:HOME_FOLDER})).toThrow(/home folder/)
  expect(()=>apply(doc,{type:'move',id:HOME_FOLDER,parentId:'a'})).toThrow(/home folder/)
  expect(()=>apply(doc,create('c','b','Operations'))).not.toThrow()
 })
 it('allows only empty folders to be deleted and never removes assignments implicitly',()=>{
  const doc=apply(apply(defaultFolders(),create('a')),create('b','a'))
  expect(()=>apply(doc,{type:'delete',id:'a'})).toThrow(/contents out/)
  const full=apply(doc,{type:'assign',packId:'my-pack',folderId:'b'})
  expect(()=>apply(full,{type:'delete',id:'b'})).toThrow(/contents out/)
  const moved=apply(full,{type:'assign',packId:'my-pack',folderId:HOME_FOLDER})
  expect(apply(moved,{type:'delete',id:'b'}).folders.some(f=>f.id==='b')).toBe(false)
 })
 it('normalizes Unicode names and rejects invisible, control and path names',()=>{
  expect(folderName('  Cafe\u0301  ')).toBe('Café')
  expect(folderName('연구開發')).toBe('연구開發')
  for(const name of ['','..','/etc','A\\B','A\u202eB','A\nB','a'.repeat(121)])expect(()=>folderName(name)).toThrow()
 })
 it('checks entire moved subtrees against the depth bound',()=>{
  let doc=defaultFolders(), parent=HOME_FOLDER
  for(let n=1;n<MAX_DEPTH;n++){doc=apply(doc,create(`n${n}`,parent));parent=`n${n}`}
  expect(()=>apply(doc,create('too-deep',parent))).toThrow(/16 levels/)
  doc=apply(doc,create('outer',null))
  expect(()=>apply(doc,{type:'move',id:'n1',parentId:'outer'})).not.toThrow()
  doc=apply(doc,create('extra','outer'))
  expect(()=>apply(doc,{type:'move',id:'n1',parentId:'extra'})).toThrow(/16 levels/)
 })
 it('fails closed on unrecognized or malformed metadata and retains safe prototype-like pack IDs',()=>{
  const doc=apply(defaultFolders(),create('a'))
  expect(packFolder(doc,'constructor')).toBe(HOME_FOLDER)
  const saved=decodeFolders(JSON.parse(JSON.stringify(apply(doc,{type:'assign',packId:'constructor',folderId:'a'}))))
  expect(packFolder(saved,'constructor')).toBe('a')
  for(const value of [{...doc,version:2},{...doc,extra:true},{...doc,assignments:{'my-pack':'missing'}},{...doc,folders:[]},{...doc,folders:[...doc.folders,doc.folders[1]]}])expect(()=>decodeFolders(value)).toThrow()
 })
 it('searches descendants only when asked and keeps All packs independent of membership',()=>{
  const doc=apply(apply(defaultFolders(),create('a')),create('b','a'))
  expect(inFolder(doc,'b','a')).toBe(false)
  expect(inFolder(doc,'b','a',true)).toBe(true)
  expect(inFolder(doc,'b','all')).toBe(true)
  expect(inFolder(doc,'a','b',true)).toBe(false)
 })
})
