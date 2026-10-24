import { Ledger } from '../../research/ledger'
import { restoreLedger } from '../../chat/checkpoint'
import { useEffect, useMemo, useSyncExternalStore } from 'react'
import { selectedAssistant } from '../target'
import { assistantReady, useAssistantSlot } from '../useAssistantSlot'
import { useEffectiveConfig } from '../../config/DeskConfigProvider'
import type { ResearchRunBinding } from '../../research/useResearchRun'
import type { Chat, ChatStore } from '../../chat/store'
import { whenSessionEnds } from '../../mcp/session'
import { savedChatRun } from './controller'
/** Saved chats consume the canonical owner. Mounting only reads/discovers. */
export function useSavedChatRun(store:ChatStore,chat:Chat):ResearchRunBinding {
 const slot=useAssistantSlot(chat.aiConnection),selected=selectedAssistant(slot),effective=useEffectiveConfig()
 const run=useMemo(()=>savedChatRun(store,chat),[store,chat.id]) // Owner lifetime; settings are saved before admission.
 useEffect(()=>{const end=whenSessionEnds(run.endSession);void run.reconnect();return()=>{end();run.disconnect()}},[run])
 const state=useSyncExternalStore(run.subscribe,run.getSnapshot,run.getSnapshot)
 const ledger=useMemo(()=>{const held=new Ledger('server-display');try{restoreLedger(held,chat.checkpoint?.sources??[])}catch{/* Original source rows remain in the retained checkpoint; unavailable rows grant no evidence. */}return held},[chat.checkpoint?.sources])
 const model=chat.model||selected?.model||''
 const blocked=!assistantReady(slot)?'Configure Assistant before starting.':!model||!selected?.models.includes(model)?'Choose an enabled model in Admin › Assistant.':''
 return {run,state,ledger,sources:ledger.sources,blocked,model,researchConfigured:effective.config.research.gateway!==null}
}
