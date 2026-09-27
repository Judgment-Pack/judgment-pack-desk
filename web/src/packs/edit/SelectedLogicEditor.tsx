import { msg } from '../../i18n'
import { Disclosure } from '../../ui/Disclosure'
import { RuleForm, ExceptionForm } from './CardForm'
import { ExtensionsBlock } from '../document/ExtensionsBlock'
import { isRecord } from '../document/MisshapenMember'
import { type LogicProjection, selectedItem } from '../logicModel'
import styles from '../inspector/LogicInspector.module.css'

/** Uses the route's existing editing session, byte buffer and explicit Save. */
export function SelectedLogicEditor({model,pointer}:{model:LogicProjection;pointer:string}) {
 const selected=selectedItem(model,pointer)
 if(!selected||!['rules','exceptions'].includes(selected.group.id))return <p>{msg('Select a rule or special case to edit.')}</p>
 return <div className={styles.details}><p className={styles.meta}>{selected.group.id==='rules'?msg('Edit rule'):msg('Edit special case')}</p><h2>{selected.item.label}</h2>
  {selected.group.id==='rules'?<RuleForm at={pointer} compact/>:<ExceptionForm at={pointer} compact/>}
  {isRecord(selected.item.value)&&isRecord(selected.item.value.extensions)&&<Disclosure title={msg('Extensions')}><ExtensionsBlock extensions={selected.item.value.extensions} at={`${pointer}/extensions`}/></Disclosure>}
 </div>
}
