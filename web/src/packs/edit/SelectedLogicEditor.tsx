import { msg } from '../../i18n'
import { Disclosure } from '../../ui/Disclosure'
import { RuleForm, ExceptionForm } from './CardForm'
import { RequirementForm } from '../document/EvidenceBlock'
import { OutcomeForm } from '../document/OutcomesBlock'
import { SourceForm } from '../document/SourcesBlock'
import { PackDocumentView } from '../document/PackDocumentView'
import type { RootMember } from '../document/members'
import { ExtensionsBlock } from '../document/ExtensionsBlock'
import { isRecord, MisshapenMember } from '../document/MisshapenMember'
import { type LogicProjection, selectedItem } from '../logicModel'
import styles from '../inspector/LogicInspector.module.css'

/** Reuses the route's editing session, held operands, byte buffer and explicit Save. */
export function SelectedLogicEditor({model,pointer,wide=false}:{model:LogicProjection;pointer:string;wide?:boolean}) {
 const selected=selectedItem(model,pointer)
 if(!selected) {
  const group=model.groups.find(group=>`/${group.id}`===pointer)
  return group ? <PackDocumentView document={model.document} active={pointer} members={[group.id as RootMember]} outline={false}/> : <p>{msg('Select an item to edit.')}</p>
 }
 if(['rules','exceptions','outcomes','sources','evidenceRequirements'].includes(selected.group.id) && !isRecord(selected.item.value)) return <MisshapenMember pointer={selected.item.pointer} label={selected.item.label} expected={msg('an object')} value={selected.item.value}/>

 const group=selected.group.id, at=selected.item.pointer
 return <div className={styles.details} data-wide={wide || undefined}>
  {!wide && <h2>{selected.item.label}</h2>}
  {group==='rules'?<RuleForm at={at} compact wide={wide}/>:group==='exceptions'?<ExceptionForm at={at} compact wide={wide}/>:
   group==='evidenceRequirements'?<RequirementForm at={at}/>:group==='outcomes'?<OutcomeForm at={at} fallback={isRecord(selected.item.value)&&model.document.fallbackOutcome===selected.item.value.id}/>:
   group==='sources'?<SourceForm at={at}/>:<PackDocumentView document={model.document} active={at} members={[at.slice(1) as RootMember]} outline={false}/>}
  {isRecord(selected.item.value)&&isRecord(selected.item.value.extensions)&&<Disclosure title={msg('Extensions')}><ExtensionsBlock extensions={selected.item.value.extensions} at={`${at}/extensions`}/></Disclosure>}
 </div>
}
