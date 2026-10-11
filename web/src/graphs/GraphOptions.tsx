import { useRef, useState } from 'react'
import { msg, useLocale } from '../i18n'
import type { GraphDocumentMeta } from '../mcp/types'
import { DefinitionJsonDialog } from '../definitions/DefinitionJsonDialog'
import { IconInfo, IconDetails } from '../shell/icons'
import { Menu, OptionsMenu } from '../ui/Menu'
import { Dialog, DialogActions } from '../ui/Dialog'
import { Button } from '../ui/Button'
import { MoveToFolderItem } from '../packs/folders/FolderBrowser'
import styles from '../packs/PackWorkspace.module.css'

export function GraphOptions({id,meta,sourceText}:{id:string;meta?:GraphDocumentMeta;sourceText?:string}) {
 useLocale()
 const trigger=useRef<HTMLButtonElement>(null), handoff=useRef(false)
 const [details,setDetails]=useState(false), [showJson,setShowJson]=useState(false)
 return <>
  <OptionsMenu label={msg('Graph options')} triggerRef={trigger} onOpenChange={open=>{if(open)handoff.current=false}} onCloseAutoFocus={event=>{if(handoff.current)event.preventDefault()}}>
   <MoveToFolderItem id={'graph:'+id} opener={trigger} onSelect={()=>{handoff.current=true}}/>
   <Menu.Item icon={<IconDetails/>} disabled={sourceText === undefined} onSelect={()=>{handoff.current=true;setShowJson(true)}}>{msg('View JSON')}</Menu.Item>
   <Menu.Separator/>
   <Menu.Item icon={<IconInfo/>} disabled={!meta} onSelect={()=>{handoff.current=true;setDetails(true)}}>{msg('Graph details…')}</Menu.Item>
  </OptionsMenu>
  <DefinitionJsonDialog open={showJson} onOpenChange={setShowJson} text={sourceText ?? ''} title={msg('Graph JSON')} openerRef={trigger}/>
  <Dialog open={details} onOpenChange={setDetails} title={msg('Graph details')} openerRef={trigger}>
   <dl className={styles.metadata}>
    <div><dt>{msg('Configured graph id')}</dt><dd>{id}</dd></div>
    <div><dt>{msg('Graph ID')}</dt><dd>{meta?.graphId || '—'}</dd></div>
    <div><dt>{msg('Version')}</dt><dd>{meta?.graphVersion || '—'}</dd></div>
    <div><dt>{msg('Path')}</dt><dd>{meta?.path || '—'}</dd></div>
   </dl>
   <DialogActions><Button onClick={()=>setDetails(false)}>{msg('Close')}</Button></DialogActions>
  </Dialog>
 </>
}
