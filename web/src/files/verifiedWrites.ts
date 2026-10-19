import {useEffect,useRef} from 'react'
import type {FileContent} from './client'
/** Same-window acknowledgement of a completed, read-back-verified host write.
 * No replay, cache or authority: consumers must still match their clean base.
 * Desk identity is frozen for the lifetime of the browser document. */
export interface VerifiedWrite {beforeSha256:string;file:FileContent}
const listeners=new Set<(receipt:VerifiedWrite)=>void>()
export function publishVerifiedWrite(receipt:VerifiedWrite){for(const listener of listeners)listener(receipt)}
export function useVerifiedWrites(onWrite:(receipt:VerifiedWrite)=>void){
 const latest=useRef(onWrite);latest.current=onWrite
 useEffect(()=>{const listener=(receipt:VerifiedWrite)=>latest.current(receipt);listeners.add(listener);return()=>{listeners.delete(listener)}},[])
}
