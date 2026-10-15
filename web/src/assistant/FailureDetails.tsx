import {msg} from '../i18n'
import {Disclosure} from '../ui/Disclosure'
import {CodeBlock} from '../ui/CodeBlock'
import {readFailure,type AIFailure} from './failures'
/** Bounded, allowlisted diagnostics shared by chat and operation activity. */
export function FailureDetails({failure,size}:{failure?:AIFailure;size?:'default'|'small'}){
 const safe=readFailure(failure)
 return safe?<Disclosure size={size} title={msg('Technical details')}><CodeBlock label={msg('Failure details')} text={JSON.stringify(safe,null,2)}/></Disclosure>:null
}
