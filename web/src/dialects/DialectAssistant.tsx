import type {DialectDraft} from './drafts'
import {ChatPlaceholder} from '../chat/ChatPlaceholder'
import {usePublishDialectEditing} from './recipeEditing'
import {useEffect,useMemo,useRef} from 'react'
import {useSearchParams} from 'react-router-dom'
import {useLocale,msg} from '../i18n'
import {useChats} from '../chat/ChatProvider'
import {ChatPanel} from '../chat/ChatPanel'
import {hasChatContent,inChatScope,type ChatScope} from '../chat/navigation'
import {useInspectorPresentation} from '../shell/InspectorPresentation'
import {useInspectorControls,useInspectorPortal} from '../shell/InspectorSlot'
import {useShellState} from '../shell/paneState'
import {useBuilderPreference} from '../builders/BuilderSplit'
import {useDialectChatContext} from './dialectChatContext'

export function DialectAssistant({id='list',selection=[],tab='records',editing=false,draft}:{draft?:DialectDraft;id?:string;selection?:string[];tab?:string;editing?:boolean}){
 usePublishDialectEditing(id,editing)
 const locale=useLocale(),shell=useShellState(),slot=useInspectorControls(),{store,ready,chats,drafts,error}=useChats()
 const [open,setOpen]=useBuilderPreference<boolean>('inputs:assistant-open',true)
 const [params]=useSearchParams(),creating=useRef(false)
 const savedContext=useDialectChatContext(id,selection,tab,editing)
 const context=draft?{kind:'input' as const,title:draft.name||msg('Untitled dialect'),detail:msg('Dialect draft'),originChatId:draft.origin?.chatId,busy:false,text:JSON.stringify({workspace:'Dialects',draftId:draft.id,name:draft.name,recipe:draft.recipe,instruction:'This is an independent, automatically saved dialect draft, not a runnable preparation. Help review its requested fields and setup. No research is authorized. Proposals do not overwrite the open draft. Do not use this draft ID with saved-dialect tools. Saving to Dialects and starting research are separate explicit actions.'})}:savedContext
 useInspectorPresentation(useMemo(()=>({title:msg('Assistant'),contextTitle:context?.title,workspaceTools:true,available:true,open,onOpenChange:setOpen,width:shell.inspectorWidth??400,onResize:shell.resizeInspector,onReset:shell.resetInspectorWidth,minimumMainWidth:480,maximumWidth:640}),[locale,context?.title,open,setOpen,shell.inspectorWidth,shell.resizeInspector,shell.resetInspectorWidth]))
 const available=[...chats,...drafts],explicit=params.get('chat')
 const scope:ChatScope={kind:'input',id,originChatId:context?.originChatId}
 const related=available.filter(chat=>inChatScope(chat,scope)&&!chat.archived).sort((a,b)=>b.updatedAt.localeCompare(a.updatedAt))
 const chat=(explicit?available.find(chat=>chat.id===explicit&&inChatScope(chat,scope)):undefined)??related.find(chat=>chat.id===context?.originChatId)??related.find(hasChatContent)??related[0]
 useEffect(()=>{
  if(!store||!ready)return
  if(chat){creating.current=false;store.activate(chat.id)}
  // Passive pane initialization must never navigate away from a draft page.
  else if(!context?.busy&&slot.open&&store.canCreate&&!creating.current){creating.current=true;store.startChat(undefined,undefined,false,undefined,undefined,{id})}
 },[store,ready,chat?.id,id,context?.busy,slot.open])
 return useInspectorPortal(chat?<ChatPanel key={chat.id} chat={chat} context={context} scope={scope} placement="pane" inputActionsLocked={editing} locked={context?.busy}/>:<ChatPlaceholder loading={!ready} error={!!error}>{ready && !store?.canCreate ? msg('Chat history is full. Export and delete an older chat from Chat history.') : msg('Open Assistant to start a conversation about this dialect.')}</ChatPlaceholder>)
}
