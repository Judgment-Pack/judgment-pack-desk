import { Select } from '../ui/Select'
import { Field } from '../ui/Field'
import type { ThinkingTier } from '../config/deskConfig'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import { connectionAssistant, deskAIConnections } from '../assistant/aiConnections'
import { allowedAgentModels } from '../config/deskConfig'
import { useEffect, useState } from 'react'
import { Slider } from 'radix-ui'
import type { AssistantAgentConfig } from '../config/deskConfig'
import type { CodexEffort } from '../assistant/agent'
import { useProviderModels } from '../assistant/providers'
import { msg } from '../i18n'
import { ModelMenuList } from '../assistant/ModelMenuList'
import { Button } from '../ui/Button'
import { Popover } from '../ui/Popover'
import { Tooltip } from '../ui/Tooltip'
import { IconChevronDown } from '../shell/icons'
import { chatReasoningAgent, type ChatReasoning } from './reasoning'
import styles from './ModelControl.module.css'

export function effortLabel(effort: CodexEffort): string {
  switch (effort) {
    case 'none': return msg('None')
    case 'minimal': return msg('Minimal')
    case 'low': return msg('Low')
    case 'medium': return msg('Medium')
    case 'high': return msg('High')
    case 'xhigh': return msg('Extra high')
    case 'max': return msg('Maximum')
  }
}
export function ModelControl({ id, model, models, agent, value, codex, enabled, disabled, onModelChange, onReasoningChange, connectionId, onConnectionChange, thinking, thinkingOverride, onThinkingChange }: {
  thinking?:ThinkingTier; thinkingOverride?:ThinkingTier; onThinkingChange?:(value:ThinkingTier|undefined)=>void
  connectionId?:string; onConnectionChange?:(id:string,name:string,model:string)=>void
  id: string; model: string; models: readonly string[]; agent?: AssistantAgentConfig; value?: ChatReasoning
  codex: boolean; enabled: boolean; disabled: boolean
  onModelChange: (model: string) => void; onReasoningChange: (value: ChatReasoning) => void
}) {
  const [open,setOpen]=useState(false), [search,setSearch]=useState('')
  const effective=useEffectiveConfig()
  const registry=effective.aiConnections?.data
  const catalog=useProviderModels(codex && enabled,connectionId)
  const choices=codex ? (catalog.data?.models??[]).filter(item=>models.includes(item.id)).map(item=>({value:item.id,label:item.id})) : models.map(value=>({value,label:value}))
  const grouped=registry?deskAIConnections(registry,effective.assistantProfile).flatMap(c=>{
    const assistant=connectionAssistant(c,effective.assistantProfile)
    const ids=assistant.engine==='codex'?allowedAgentModels(assistant.agent):assistant.endpoint?.models??[]
    return ids.map(id=>({value:JSON.stringify([c.id,id]),label:id,group:c.name}))
  }):undefined
  const pick=(value:string)=>{if(grouped){const [id,model]=JSON.parse(value) as [string,string];const c=registry?.connections.find(c=>c.id===id);if(c)onConnectionChange?.(id,c.name,model)}else onModelChange(value)}
  const advertised=catalog.data?.models.find(item=>item.id===model)
  const effort=chatReasoningAgent(agent,model,value)?.effort
  const current=effort??advertised?.defaultEffort
  const index=current ? advertised?.efforts.indexOf(current)??-1 : -1
  const adjustable=codex && enabled && advertised && advertised.efforts.length>1
  useEffect(()=>{if(disabled)setOpen(false)},[disabled])
  return <Popover title={msg('Model')} variant="list" size="small" align="start" open={open} onOpenChange={value=>{setOpen(value);if(!value)setSearch('')}}
    trigger={<button id={id} type="button" className={styles.trigger} disabled={disabled} aria-label={`${msg('Model')}: ${model}${codex&&current?`, ${msg('Reasoning')}: ${effortLabel(current)}`:''}`}>
      <span className={styles.model}>{model||msg('Choose model')}</span>{codex&&current&&<span className={styles.depth}>· {effortLabel(current)}</span>}<IconChevronDown/>
    </button>}>
    <ModelMenuList pending={!grouped&&codex&&(catalog.isPending||catalog.isError)} options={grouped??choices} search={search} onSearch={setSearch} value={grouped?JSON.stringify([connectionId,model]):model} onValueChange={pick} disabled={disabled||!grouped&&codex&&(!enabled||catalog.isPending||catalog.isError)}/>
    {!codex&&onThinkingChange&&<div className={styles.settings}>
      <Field label={msg('Reasoning effort')}>{w=><Select {...w} value={thinkingOverride??thinking??'off'} disabled={disabled} onValueChange={v=>onThinkingChange(v as ThinkingTier)} options={[{value:'off',label:msg('off')},{value:'on',label:msg('standard')},{value:'ultra',label:msg('deep')}]}/>}</Field>
      <Button variant="quiet" disabled={thinkingOverride===undefined||disabled} onClick={()=>onThinkingChange(undefined)}>{msg('Reset to default')}</Button>
      <span className={styles.caption}>{registry?.connections.find(c=>c.id===connectionId)?.name}</span>
    </div>}
    {codex&&<div className={styles.settings}>
      {codex&&catalog.isPending&&<span className={styles.caption} role="status">{msg('Loading models…')}</span>}
      {codex&&catalog.isError&&<div className={styles.caption} role="alert">{msg(catalog.error.message)} <Button variant="inline" onClick={()=>void catalog.refetch()}>{msg('Retry')}</Button></div>}
      {adjustable&&<div className={styles.reasoning}>
        <div className={styles.heading}><label id={`${id}-effort`}>{msg('Reasoning')}</label><span className={styles.level}>{index<0?msg('Unavailable'):effortLabel(current!)}</span>
          <Tooltip content={msg('Reset to default')}><Button variant="quiet" size="icon" disabled={disabled||catalog.isError||effort===undefined} aria-label={msg('Reset to default')} onClick={()=>onReasoningChange({model,effort:null})}>
            <svg aria-hidden="true" width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" strokeLinejoin="round"><path d="M2.5 5.5A5.5 5.5 0 1 1 2.7 11M2.5 2v3.5H6"/></svg>
          </Button></Tooltip>
        </div>
        <Slider.Root className={styles.slider} value={[Math.max(index,0)]} min={0} max={advertised.efforts.length-1} step={1} disabled={disabled||catalog.isError||index<0}
          onValueChange={([next])=>{const chosen=advertised.efforts[next!];if(chosen)onReasoningChange({model,effort:chosen})}}>
          <Slider.Track className={styles.track}><Slider.Range className={styles.range}/></Slider.Track>
          <div className={styles.stops} aria-hidden="true">{advertised.efforts.map(level=><span key={level}/>)}</div>
          <Slider.Thumb className={styles.thumb} aria-labelledby={`${id}-effort`} aria-valuetext={index>=0&&current?effortLabel(current):msg('Unavailable')}/>
        </Slider.Root>
        <div className={styles.scale} aria-hidden="true"><span>{effortLabel(advertised.efforts[0]!)}</span><span>{effortLabel(advertised.efforts.at(-1)!)}</span></div>
      </div>}
      {codex&&<span className={styles.caption}>{registry?.connections.find(c=>c.id===connectionId)?.name??msg('ChatGPT · Codex')}</span>}
    </div>}
  </Popover>
}
