import {ChatPlaceholder} from '../chat/ChatPlaceholder'
import {usePublishDialectEditing} from './recipeEditing'
import {useEffect,useMemo,useRef} from 'react'
import {useLocation,useNavigate,useSearchParams} from 'react-router-dom'
import {useLocale,msg} from '../i18n'
import {useChats} from '../chat/ChatProvider'
import {ChatPanel} from '../chat/ChatPanel'
import {assistantChatHref,hasChatContent,inChatScope,type ChatScope} from '../chat/navigation'
import {useInspectorPresentation} from '../shell/InspectorPresentation'
import {useInspectorControls,useInspectorPortal} from '../shell/InspectorSlot'
import {useShellState} from '../shell/paneState'
import {useBuilderPreference} from '../builders/BuilderSplit'
import {useDialectChatContext} from './dialectChatContext'

export function DialectAssistant({id='list',selection=[],tab='records',editing=false}:{id?:string;selection?:string[];tab?:string;editing?:boolean}){
 usePublishDialectEditing(id,editing)
 const locale=useLocale(),shell=useShellState(),slot=useInspectorControls(),{store,ready,chats,drafts,error}=useChats()
 const [open,setOpen]=useBuilderPreference<boolean>('inputs:assistant-open',true)
 const [params]=useSearchParams(),location=useLocation(),navigate=useNavigate(),creating=useRef(false)
 const context=useDialectChatContext(id,selection,tab,editing)
 useInspectorPresentation(useMemo(()=>({title:msg('Assistant'),contextTitle:context?.title,workspaceTools:true,available:true,open,onOpenChange:setOpen,width:shell.inspectorWidth??400,onResize:shell.resizeInspector,onReset:shell.resetInspectorWidth,minimumMainWidth:480,maximumWidth:640}),[locale,context?.title,open,setOpen,shell.inspectorWidth,shell.resizeInspector,shell.resetInspectorWidth]))
 const available=[...chats,...drafts],explicit=params.get('chat')
 const scope:ChatScope={kind:'input',id,originChatId:context?.originChatId}
 const related=available.filter(chat=>inChatScope(chat,scope)&&!chat.archived).sort((a,b)=>b.updatedAt.localeCompare(a.updatedAt))
 const chat=(explicit?available.find(chat=>chat.id===explicit&&inChatScope(chat,scope)):undefined)??related.find(hasChatContent)??related[0]
 useEffect(()=>{
  if(!store||!ready)return
  if(chat){creating.current=false;store.activate(chat.id)}
  else if(!context?.busy&&slot.open&&store.canCreate&&!creating.current){creating.current=true;const next=store.startChat(undefined,undefined,false,undefined,undefined,{id});navigate(assistantChatHref(next,location),{replace:true})}
 },[store,ready,chat?.id,id,context?.busy,slot.open,navigate,location.pathname,location.search])
 return useInspectorPortal(chat?<ChatPanel key={chat.id} chat={chat} context={context} scope={scope} placement="pane" inputActionsLocked={editing} locked={context?.busy}/>:<ChatPlaceholder loading={!ready} error={!!error}>{ready && !store?.canCreate ? msg('Chat history is full. Export and delete an older chat from Chat history.') : msg('Open Assistant to start a conversation about this dialect.')}</ChatPlaceholder>)
}
