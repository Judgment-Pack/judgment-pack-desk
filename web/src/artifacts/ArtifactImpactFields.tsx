import {msg} from '../i18n'
import {Field,FieldGroup} from '../ui/Field'
import {Input} from '../ui/Input'
import {Select} from '../ui/Select'
import type {ArtifactImpact} from './review'
export function artifactImpactLabel(value:ArtifactImpact['classification']) {
 switch(value){
  case 'initial':return msg('First publication')
  case 'patch':return msg('Compatible fixes')
  case 'minor':return msg('Compatible additions')
  case 'major':return msg('Breaking changes')
  default:return msg('Needs review')
 }
}
/** The reviewer classifies compatibility; Runtime supplies version arithmetic. */
export function ArtifactImpactFields({value,onChange,disabled=false}:{value:ArtifactImpact;onChange:(next:ArtifactImpact)=>void;disabled?:boolean}) {
 return <FieldGroup><Field label={msg('Compatibility impact')}>{props=><Select {...props} value={value.classification} disabled={disabled} onValueChange={classification=>onChange({...value,classification:classification as ArtifactImpact['classification']})} options={(['unknown','initial','patch','minor','major'] as const).map(value=>({value,label:artifactImpactLabel(value)}))}/>}</Field><Field label={msg('Reason for this version')}>{props=><Input {...props} value={value.rationale} maxLength={4000} disabled={disabled} onChange={event=>onChange({...value,rationale:event.target.value})}/>}</Field></FieldGroup>
}
