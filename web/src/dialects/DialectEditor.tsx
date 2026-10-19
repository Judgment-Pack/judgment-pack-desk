import {useId} from 'react'
import {msg} from '../i18n'
import {factFields} from '../packs/test-workspace/model'
import {humanId} from '../packs/logicModel'
import {Select} from '../ui/Select'
import {Input} from '../ui/Input'
import {Field} from '../ui/Field'
import {Disclosure} from '../ui/Disclosure'
import {setBinding} from '../jobs/dialectEditorModel'
import type {DialectConversion,DialectPackBinding,DialectRecord} from './client'
import {boundPack,recordColumns} from './qualification'
import styles from './DialectsView.module.css'
import type {ResearchRecipe} from './research'

/** Supplied-dialect recipe editor. Shared controls, wire-compatible case mappings;
 * conversions are separate recipe data and never added to mapping-v2. */
export function DialectEditor({value,records,research,disabled,onChange}:{value:DialectPackBinding;records:DialectRecord[];research?:ResearchRecipe;disabled:boolean;onChange:(value:DialectPackBinding)=>void}){
 const id=useId()
 const pack=boundPack(value),fields=factFields(pack),columns=recordColumns(records,research)
 const options=[{value:'omitted',label:msg('Not supplied')},...columns]
 const change=(kind:'facts'|'evidence',target:string,source:string)=>{
  const mapping=setBinding(value.mapping,kind,target,source==='omitted'?'omitted':'case',source==='omitted'?'':source)
  const conversions={...value.conversions};if(source==='omitted')delete conversions[target]
  onChange({...value,mapping,conversions})
 }
 const conversion=(target:string,next:DialectConversion)=>onChange({...value,conversions:{...value.conversions,[target]:next}})
 return <section className={styles.stack} aria-label={msg('Dialect')}>
  <div className={styles.tableWrap}><table className={styles.table}><thead><tr><th>{msg('Pack fact')}</th><th>{msg('Source column')}</th><th>{msg('Use as')}</th></tr></thead><tbody>{fields.map(field=>{
   const source=value.mapping.case?.facts?.find(f=>f.target===field.path)?.source??'omitted',rule=value.conversions?.[field.path]??{kind:'text'}
   return <tr key={field.path}><td>{field.label}</td><td><Select id={id+field.path+'-source'} aria-label={msg('Source for {{name}}',{name:field.label})} disabled={disabled} value={source} options={options} onValueChange={next=>change('facts',field.path,next)}/></td><td><Select id={id+field.path+'-conversion'} aria-label={msg('Use {{name}} as',{name:field.label})} disabled={disabled||source==='omitted'} value={rule.kind} options={[{value:'text',label:msg('Text')},{value:'research-value',label:msg('Research value')},{value:'decimal',label:msg('Decimal')},{value:'number',label:msg('JSON number')},{value:'boolean',label:msg('Yes / No')}]} onValueChange={kind=>conversion(field.path,kind==='boolean'?{kind,trueValue:'yes',falseValue:'no'}:{kind:kind as DialectConversion['kind']})}/></td></tr>
  })}</tbody></table></div>
  {fields.filter(field=>value.conversions?.[field.path]?.kind==='boolean').map(field=>{const rule=value.conversions![field.path];return <fieldset className={styles.booleanRule} key={field.path}><legend>{field.label}</legend><Field label={msg('Yes value')} hint={msg('Exact match, including case.')}>{props=><Input {...props} disabled={disabled} maxLength={160} value={rule.trueValue??''} onChange={e=>conversion(field.path,{...rule,trueValue:e.target.value})}/>}</Field><Field label={msg('No value')}>{props=><Input {...props} disabled={disabled} maxLength={160} value={rule.falseValue??''} onChange={e=>conversion(field.path,{...rule,falseValue:e.target.value})}/>}</Field></fieldset>})}
  {!Object.values(value.conversions??{}).some(rule=>rule.kind==='research-value')&&<p className={styles.note}>{msg('Original values are kept. Blank values remain unknown. Use Decimal for ordered numeric comparisons; JSON number preserves numeric JSON fields. Conversions do not infer values from prose.')}</p>}
  {Object.values(value.conversions??{}).some(rule=>rule.kind==='research-value')&&<p className={styles.note}>{msg('Research value preserves the original field type, including empty text, zero and false. Only declared research fields support it. Imported columns and record metadata need another conversion. Missing or unresolved fields remain unknown.')}</p>}
  {Boolean(pack.evidenceRequirements?.length)&&<Disclosure title={msg('Evidence availability')}><div className={styles.stack}>{pack.evidenceRequirements!.map(e=><Field key={e.id} label={humanId(e.id,e.id)} hint={msg('Use present, absent or unknown. Supplied evidence is not independently verified.')}>{props=><Select {...props} disabled={disabled} value={value.mapping.case?.evidence?.find(row=>row.requirement===e.id)?.source??'omitted'} options={options} onValueChange={next=>change('evidence',e.id,next)}/>}</Field>)}</div></Disclosure>}
 </section>
}
