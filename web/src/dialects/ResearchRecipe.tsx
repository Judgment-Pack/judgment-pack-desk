import {useRef} from 'react'
import {ResearchExecutionSettings} from './ResearchExecution'
import {ResearchRetrySettings} from './ResearchRetries'
import {defaultResearchRetries,requiresResearchParents} from './research'
import {ResearchCompanies} from './ResearchCompanies'
import {msg} from '../i18n'
import {useEffectiveConfig} from '../config/DeskConfigProvider'
import {deskAIConnections,resolveAIConnection} from '../assistant/aiConnections'
import {useSearchConnections} from '../search/connections'
import {Button,ButtonLink} from '../ui/Button'
import {Field,FieldGroup} from '../ui/Field'
import {useConfirmDiscard} from '../shell/UnsavedChanges'
import {Input} from '../ui/Input'
import {TextArea} from '../ui/TextArea'
import {RecipeSection} from './RecipeSection'
import {RecipeFields} from './RecipeFields'
import section from './RecipeSection.module.css'
import {Select} from '../ui/Select'
import {Disclosure} from '../ui/Disclosure'
import type {DialectRecord} from './client'
import type {ResearchRecipe as Recipe} from './research'
import styles from './DialectsView.module.css'

export function ResearchRecipe({value,onChange,records,disabled,allowRemove=true,companySelection=false,section:visible='all'}:{companySelection?:boolean;section?:'all'|'overview'|'sources'|'execution';value?:Recipe;onChange:(value?:Recipe)=>void;records:DialectRecord[];disabled:boolean;allowRemove?:boolean}){
 const effective=useEffectiveConfig(),searches=useSearchConnections(),confirm=useConfirmDiscard(),executionSection=useRef<HTMLElement>(null),companySection=useRef<HTMLElement>(null)
 const connections=effective.aiConnections?.data?deskAIConnections(effective.aiConnections.data,effective.assistantProfile):[]
 const resolved=resolveAIConnection(effective,value?.ai.connection),assistant=resolved.assistant
 const modelConfig=assistant.engine==='codex'?assistant.agent:assistant.endpoint
 const models=modelConfig?.models??(modelConfig?.model?[modelConfig.model]:[])
 const chooseConnection=(id?:string)=>{const next=resolveAIConnection(effective,id);const config=next.assistant.engine==='codex'?next.assistant.agent:next.assistant.endpoint;return {connection:next.connection?.id??'',revision:next.connection?.revision??'',model:config?.model??'',...(next.assistant.engine==='codex'&&next.assistant.agent?.effort?{effort:next.assistant.agent.effort}:{})}}
 if(!value)return <section className={styles.stack} aria-label={msg('Research')}><strong>{msg('Research')}</strong><p className={styles.note}>{msg('Find records before choosing a pack, or use your pack’s requirements to guide research.')}</p><div><Button disabled={disabled} onClick={()=>onChange({version:1,retries:defaultResearchRetries(),target:'records',fields:[{name:'description',description:msg('Description'),type:'string'}],criteria:[],ai:chooseConnection(),limits:{records:25,searches:0,pages:30,modelCalls:30}})}>{msg('Set up research')}</Button></div></section>
 const edit=(change:Partial<Recipe>)=>onChange({...value,...change})
 const searchConnection=searches.available&&!searches.isError?searches.data?.connections.find(c=>c.id===value.search?.connection):undefined
 const aiReady=!!value.ai.connection&&!!value.ai.model&&!!value.ai.revision
 const aiChanged=!!resolved.connection&&resolved.connection.revision!==value.ai.revision
 const searchChanged=!!searchConnection&&searchConnection.revision!==value.search?.revision
 const companyCount=(value.companyIds?.length??0)+(value.companyInputs?.length??0)
 const selectCompanies=companySelection||requiresResearchParents(value)
 const companyMissing=selectCompanies&&!companyCount
 const searchProvider=searches.data?.providers?.find(provider=>provider.id===searchConnection?.provider)
 return <section className={section.stack} aria-label={msg('Research')}>
  {visible==='all'&&(!aiReady||companyMissing)&&<p className={section.attention} role="status">{msg('Setup needed')} · {!aiReady?msg('Choose an AI connection and model.'):value.target==='records'?msg('Select source records for related research.'):msg('Select companies to research.')} <Button variant="inline" size="small" disabled={disabled} onClick={()=>{const element=(!aiReady?executionSection:companySection).current;element?.scrollIntoView({block:'center',behavior:'smooth'});element?.focus({preventScroll:true})}}>{msg('Review settings')}</Button></p>}
  {(visible==='all'||visible==='overview')&&<>
  {selectCompanies&&<RecipeSection title={value.target==='records'?msg('Selected records'):msg('Selected companies')} sectionRef={companySection}><ResearchCompanies value={value} records={records} disabled={disabled} onChange={change=>edit({requireParents:true,...change})}/></RecipeSection>}
  {visible==='all'&&<RecipeFields value={value.fields} onChange={fields=>edit({fields})} disabled={disabled}/>}
  {value.criteria.length>0&&<RecipeSection title={msg('Research criteria')}><div className={styles.stack}><p className={styles.note}>{msg('These instructions guide research. They do not change qualification rules.')}</p>{value.criteria.map((criterion,index)=><Field key={criterion.id} label={criterion.id} hint={criterion.qualificationTarget?msg('Suggested pack input: {{target}}',{target:criterion.qualificationTarget}):undefined}>{props=><TextArea {...props} rows={3} disabled={disabled} maxLength={2000} value={criterion.description} onChange={event=>edit({criteria:value.criteria.map((c,i)=>i===index?{...c,description:event.target.value}:c)})}/>}</Field>)}</div></RecipeSection>}
  </>}
  {(visible==='all'||visible==='sources')&&<RecipeSection title={msg('Sources')} attention={searchChanged||!!value.search&&!searchConnection&&!searches.loading}>
  <Field label={msg('Web search')} hint={msg('Use an existing search connection, or research supplied URLs only.')}>{props=><Select {...props} disabled={disabled||searches.loading} value={value.search?.connection??'provided'} options={[{value:'provided',label:msg('Supplied URLs only')},...(searches.data?.connections??[]).map(c=>({value:c.id,label:c.name}))]} onValueChange={id=>{const c=searches.data?.connections.find(c=>c.id===id);edit({search:c?{connection:c.id,revision:c.revision}:undefined,limits:{...value.limits,searches:c?10:0}})}}/>}</Field>
  {searchConnection&&searchConnection.revision!==value.search?.revision&&<div><p className={styles.note}>{msg('Search settings changed. Review the current settings.')}</p><Button disabled={disabled||searches.loading||searches.isFetching} onClick={()=>edit({search:{connection:searchConnection.id,revision:searchConnection.revision}})}>{msg('Use current settings')}</Button></div>}
  {(searches.isError||!searches.available)&&<p className={styles.note}>{msg('Search connections are unavailable. Supplied URLs can still be used.')}</p>}
  {searchProvider?.kind==='grounded-answer'&&<p className={styles.note}>{msg('Grounded search returns selected sources, not every search result. Coverage depends on the provider.')}</p>}
  </RecipeSection>
  }
  {(visible==='all'||visible==='execution')&&<RecipeSection title={msg('Execution')} sectionRef={executionSection} attention={aiChanged||aiReady&&!!resolved.problem}>
  <FieldGroup>
  <Field label={msg('AI connection')}>{props=><Select {...props} disabled={disabled} value={value.ai.connection} placeholder={msg('Choose a connection')} options={connections.map(c=>({value:c.id,label:c.name}))} onValueChange={id=>edit({ai:chooseConnection(id)})}/>}</Field>
  {resolved.problem&&value.ai.connection&&<p className={styles.note}>{resolved.problem} <ButtonLink to="/connections?tab=ai" variant="quiet">{msg('Manage connections')}</ButtonLink></p>}
  <Field label={msg('Model')}>{props=><Select {...props} disabled={disabled||!resolved.connection} value={value.ai.model} placeholder={msg('Choose a model')} options={models.map(model=>({value:model,label:model}))} onValueChange={model=>edit({ai:{...value.ai,revision:resolved.connection!.revision,model}})}/>}</Field>
  {resolved.connection&&resolved.connection.revision!==value.ai.revision&&<div><p className={styles.note}>{msg('This connection changed. Review its model settings before research.')}</p><Button disabled={disabled} onClick={()=>edit({ai:chooseConnection(value.ai.connection)})}>{msg('Use current settings')}</Button></div>}
  {assistant.engine==='codex'&&<Field label={msg('Reasoning')}>{props=><Select {...props} disabled={disabled} value={value.ai.effort??'default'} options={[{value:'default',label:msg('Provider default')},...['minimal','low','medium','high','xhigh','max'].map(value=>({value,label:value}))]} onValueChange={effort=>edit({ai:{...value.ai,effort:effort==='default'?undefined:effort}})}/>}</Field>}
  <ResearchExecutionSettings value={value.execution} connectionLimit={assistant.engine==='codex'?1:2} disabled={disabled||!resolved.connection} onChange={execution=>edit({execution})}/>
  </FieldGroup>
  <RecipeSection title={msg('Limits and retries')}><div className={styles.stack}><FieldGroup className={styles.researchLimits}>{([{key:'records',label:msg('Records'),min:1,max:50},{key:'searches',label:msg('Searches'),min:0,max:30},{key:'pages',label:msg('Page reads'),min:0,max:100},{key:'modelCalls',label:msg('Model requests'),min:1,max:60}] as const).map(limit=><Field key={limit.key} label={limit.label} hint={limit.key==='records'?msg('Maximum records per run. This setting takes precedence over record counts in the instructions.'):limit.key==='modelCalls'?msg('AI requests plan searches and page reads, process results and save records. Up to two requests are reserved for final saves.'):undefined}>{props=><Input {...props} type="number" disabled={disabled||limit.key==='searches'&&!value.search} min={limit.min} max={limit.max} value={value.limits[limit.key]} onChange={event=>edit({limits:{...value.limits,[limit.key]:Number(event.target.value)}})}/>}</Field>)}</FieldGroup><ResearchRetrySettings value={value.retries} onChange={retries=>edit({retries})} disabled={disabled}/></div></RecipeSection>
  </RecipeSection>
  }
  {allowRemove&&<Disclosure title={msg('Research settings')}><div className={styles.removeSection}><Button variant="quiet" disabled={disabled} onClick={async()=>{if(await confirm(msg('Remove the research setup from this recipe? This change takes effect when you save.')))onChange(undefined)}}>{msg('Remove research setup')}</Button></div></Disclosure>}
 </section>
}
