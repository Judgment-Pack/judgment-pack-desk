import { msg, useLocale } from '../i18n'
import { Notice } from '../ui/Notice'
import {ChatAction} from './ChatActions'
export function ResearchSuggestion({disabled,onChoose}:{disabled:boolean;onChoose:(choice:'research'|'provided')=>void}) {
 useLocale()
 return <Notice title={msg('Use Research for this request?')} actions={<><ChatAction variant="inline" disabled={disabled} onClick={()=>onChoose('research')}>{msg('Use Research')}</ChatAction><ChatAction variant="inline" disabled={disabled} onClick={()=>onChoose('provided')}>{msg('Keep Chat')}</ChatAction></>}>
  {msg('Search the web, compare sources, and include citations. This request only.')}
 </Notice>
}
