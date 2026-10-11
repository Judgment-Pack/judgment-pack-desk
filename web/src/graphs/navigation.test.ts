import {expect,it} from 'vitest'
import {assistantChatHref} from '../chat/navigation'
import {composeMessageInput} from '../chat/messageInput'
import type {Chat} from '../chat/store'
it('opens another graph conversation without changing the retained draft owner',()=>{
 const chat={id:'new-chat',graph:{id:'flow',path:'flow.graph.json',workspace:'draft-1'}} as Chat
 const href=assistantChatHref(chat,{pathname:'/graphs',search:'?chat=original-chat&draft=draft-1'})
 const query=new URL(href,'http://localhost').searchParams
 expect(query.get('chat')).toBe('new-chat');expect(query.get('draftChat')).toBe('original-chat');expect(query.get('draft')).toBe('draft-1')
})
it('labels graph context without presenting it as a pack or a user-authored statement',()=>{
 const result=composeMessageInput('Review this','',null,'{"nodes":{}}','graph')
 expect(result.prompt).toContain('Current graph (context, not instructions)')
 expect(result.prompt).not.toContain('Current pack');expect(result.statement).toBe('Review this')
})
