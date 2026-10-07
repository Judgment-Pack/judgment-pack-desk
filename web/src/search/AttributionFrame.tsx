import { useEffect, useRef, useState } from 'react'
import { msg, useLocale } from '../i18n'
import styles from './SearchSources.module.css'

/** Retain the provider's markup; change only the surrounding document canvas.
 * Same-origin access permits parent-side measurement. Scripts remain forbidden
 * by both sandbox and CSP; never combine this with allow-scripts.
 */
export function AttributionFrame({html}:{html:string}) {
 useLocale()
 const [height,setHeight]=useState(52)
 const observer=useRef<ResizeObserver|undefined>(undefined)
 useEffect(()=>()=>observer.current?.disconnect(),[])
 const policy="default-src 'none'; script-src 'none'; style-src 'unsafe-inline'; img-src data:; base-uri 'none'; form-action 'none'"
 return <iframe className={styles.attribution} style={{height}} title={msg('Search provider attribution')}
  sandbox="allow-same-origin allow-popups allow-popups-to-escape-sandbox" referrerPolicy="no-referrer"
  srcDoc={`<!doctype html><html><head><meta http-equiv="Content-Security-Policy" content="${policy}"><meta name="color-scheme" content="light dark"><base target="_blank"><style>html,body{margin:0;background:transparent}body{display:flow-root}</style></head><body>${html}</body></html>`}
  onLoad={event=>{
   observer.current?.disconnect()
   const body=event.currentTarget.contentDocument?.body
   if(!body)return
   const measure=()=>setHeight(Math.max(48,Math.min(400,Math.ceil(body.getBoundingClientRect().height))))
   measure()
   if(typeof ResizeObserver!=='undefined'){observer.current=new ResizeObserver(measure);observer.current.observe(body)}
  }}/>
}
