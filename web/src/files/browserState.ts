import type { FileEntry } from './client'
export interface FileNode { path: string; name: string; directory: boolean; children: FileNode[] }
export function fileTree(files: readonly FileEntry[]): FileNode[] {
 const nodes=new Map<string,FileNode>(),roots:FileNode[]=[]
 for(const file of files){
  const parts=file.path.split('/');let siblings=roots
  for(let i=0;i<parts.length;i++){
   const path=parts.slice(0,i+1).join('/');let node=nodes.get(path)
   if(!node){node={path,name:parts[i]!,directory:i<parts.length-1,children:[]};nodes.set(path,node);siblings.push(node)}
   siblings=node.children
  }
 }
 const sort=(rows:FileNode[])=>{rows.sort((a,b)=>Number(b.directory)-Number(a.directory)||a.name.localeCompare(b.name,undefined,{numeric:true}));rows.forEach(row=>sort(row.children))}
 sort(roots);return roots
}
export function parents(path: string): string[] {const parts=path.split('/');return parts.slice(0,-1).map((_,i)=>parts.slice(0,i+1).join('/'))}
export interface FileBrowserPreferences { selected?:string; expanded?:string[]; search:string; scroll:number; collapsed:boolean; wrap:boolean }
const defaults:FileBrowserPreferences={search:'',scroll:0,collapsed:false,wrap:false}
export const fileBrowserKey=(root:string)=>`jpack.files-browser.v1:${root}`
export function readFileBrowser(root:string):FileBrowserPreferences {
 try{
  const value=JSON.parse(localStorage.getItem(fileBrowserKey(root))??'null')
  if(!value||typeof value!=='object')return {...defaults}
  return {selected:typeof value.selected==='string'?value.selected:undefined,
   expanded:Array.isArray(value.expanded)?value.expanded.filter((s:unknown)=>typeof s==='string').slice(0,10000):undefined,
   search:typeof value.search==='string'?value.search.slice(0,256):'',scroll:Number.isFinite(value.scroll)?Math.max(0,value.scroll):0,collapsed:value.collapsed===true,wrap:value.wrap===true}
 }catch{return {...defaults}}
}
export function writeFileBrowser(root:string,value:FileBrowserPreferences){try{localStorage.setItem(fileBrowserKey(root),JSON.stringify(value))}catch{/* Browser preferences are optional. */}}
