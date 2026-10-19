import {useLayoutEffect,useSyncExternalStore} from 'react'
// Readers can replace the chat pane. Keep the actual editor lock observable
// there too, rather than relying on a prop captured when a reader opened.
const editing=new Map<string,Set<symbol>>()
const listeners=new Set<()=>void>()
const subscribe=(listener:()=>void)=>{listeners.add(listener);return ()=>{listeners.delete(listener)}}
const emit=()=>{for(const listener of listeners)listener()}
export function usePublishDialectEditing(id:string,dirty:boolean){
 useLayoutEffect(()=>{
  if(!dirty)return
  const token=Symbol(),owners=editing.get(id)??new Set<symbol>();owners.add(token);editing.set(id,owners);emit()
  return ()=>{owners.delete(token);if(!owners.size)editing.delete(id);emit()}
 },[id,dirty])
}
export function useDialectRecipeEditing(id:string){return useSyncExternalStore(subscribe,()=>!!editing.get(id)?.size,()=>false)}

export const dialectRecipeIsEditing=(id:string)=>!!editing.get(id)?.size
