import {useLayoutEffect,useSyncExternalStore} from 'react'
const owners=new Map<string,Set<symbol>>(),listeners=new Set<()=>void>()
const emit=()=>{for(const listener of listeners)listener()}
const subscribe=(listener:()=>void)=>{listeners.add(listener);return ()=>{listeners.delete(listener)}}
export const policyEditorBlocked=(id:string)=>!!owners.get(id)?.size
export const usePolicyEditorBlocked=(id:string)=>useSyncExternalStore(subscribe,()=>policyEditorBlocked(id),()=>false)
/** Publish the live editor guard to both pane and main-chat proposal controls. */
export function usePublishPolicyEditing(id:string,blocked:boolean){useLayoutEffect(()=>{if(!blocked)return;const token=Symbol(),set=owners.get(id)??new Set<symbol>();set.add(token);owners.set(id,set);emit();return ()=>{set.delete(token);if(!set.size)owners.delete(id);emit()}},[id,blocked])}
