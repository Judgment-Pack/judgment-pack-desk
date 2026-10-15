import {msg} from '../i18n'
import {IconRetry} from '../shell/icons'
import {ChatAction} from './ChatActions'

/** One recovery command in the transcript and interrupted-response row. */
export function RetryResponseButton({disabled,onClick,additionalRequests}:{disabled?:boolean;onClick:()=>void;additionalRequests?:number}) {
 return <ChatAction disabled={disabled} onClick={onClick}><IconRetry/>{additionalRequests?msg('Retry · +{{count}} requests',{count:additionalRequests}):msg('Retry response')}</ChatAction>
}
