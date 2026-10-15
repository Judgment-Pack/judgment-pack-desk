import {useQuery} from '@tanstack/react-query'
import {answer,deskFetch} from '../../files/client'
import {privateQuery} from '../../data/privateQuery'
export interface ActionGrant {id:string;action:string;chatId:string;path:string;revision:string;name:string;state:'ready'|'used'|'expired'|'revoked'|'stale';expiresAt:string}
export interface ActionApproval {mode:'manual'|'auto';grant?:ActionGrant}
export const actionApprovalKey=(chat:string)=>['assistant-approvals',chat]
export const actionApprovalAPI=async<T,>(chat:string,operation='',body?:unknown)=>answer<T>(await deskFetch('/api/assistant-approvals/'+encodeURIComponent(chat)+(operation?'/'+operation:''),{method:body===undefined?'GET':'POST',headers:{'Content-Type':'application/json'},...(body===undefined?{}:{body:JSON.stringify(body)})}))
export function useActionApproval(chat:string,enabled=true){return useQuery({queryKey:actionApprovalKey(chat),queryFn:()=>actionApprovalAPI<ActionApproval>(chat),...privateQuery,enabled})}
