import {useEffect,useMemo,useRef} from 'react'
import {useSearchParams} from 'react-router-dom'
import {msg,useLocale} from '../i18n'
import {useChats} from '../chat/ChatProvider'
import {ChatPanel} from '../chat/ChatPanel'
import {ChatPlaceholder} from '../chat/ChatPlaceholder'
import {useInspectorPresentation} from '../shell/InspectorPresentation'
import {useInspectorControls,useInspectorPortal} from '../shell/InspectorSlot'
import {useShellState} from '../shell/paneState'
import {useBuilderPreference} from '../builders/BuilderSplit'
import {usePublishPolicyEditing} from './chatEditing'
export function PolicyAssistant({id,editing=false,busy=false}:{id:string;editing?:boolean;busy?:boolean}){
 usePublishPolicyEditing(id,editing||busy)
 const locale=useLocale(),shell=useShellState(),slot=useInspectorControls(),{store,ready,chats,drafts,error}=useChats()
 const [open,setOpen]=useBuilderPreference<boolean>('policies:assistant-open',true),[params]=useSearchParams(),creating=useRef(false)
 useInspectorPresentation(useMemo(()=>({title:msg('Assistant'),contextTitle:msg('Policy'),workspaceTools:true,available:true,open,onOpenChange:setOpen,width:shell.inspectorWidth??400,onResize:shell.resizeInspector,onReset:shell.resetInspectorWidth,minimumMainWidth:480,maximumWidth:640}),[locale,open,setOpen,shell.inspectorWidth,shell.resizeInspector,shell.resetInspectorWidth]))
 const available=[...chats,...drafts],explicit=params.get('chat'),related=available.filter(chat=>chat.policy?.id===id&&!chat.archived).sort((a,b)=>b.updatedAt.localeCompare(a.updatedAt))
 const chat=(explicit?available.find(chat=>chat.id===explicit&&chat.policy?.id===id):undefined)??related[0]
 useEffect(()=>{if(!store||!ready)return;if(chat){creating.current=false;store.activate(chat.id)}else if(slot.open&&store.canCreate&&!creating.current){creating.current=true;store.startChat(undefined,undefined,false,undefined,undefined,undefined,{id})}},[store,ready,chat?.id,id,slot.open])
 return useInspectorPortal(chat?<ChatPanel key={chat.id} chat={chat} placement="pane" scope={{kind:'policy',id}} locked={busy}/>:<ChatPlaceholder loading={!ready} error={!!error}>{ready&&!store?.canCreate?msg('Chat history is full. Export and delete an older chat from Chat history.'):msg('Open Assistant to start a conversation about this policy.')}</ChatPlaceholder>)
}
