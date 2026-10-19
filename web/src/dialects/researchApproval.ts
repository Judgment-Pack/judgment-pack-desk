import {useQuery} from '@tanstack/react-query'
import {answer,deskFetch} from '../files/client'
import type {ResearchLimits} from './research'
export interface ResearchAllowance {runId:string;preparationId:string;name:string;recipeVersion:number;limits:ResearchLimits;state:'ready'|'used'|'revoked'|'expired'|'stale';approvedAt:string;expiresAt:string;usedAt?:string;revokedAt?:string}
export interface ResearchApprovalStatus {mode:'manual'|'auto';grant?:ResearchAllowance}
export const approvalKey=(chatId:string)=>['research-approval',chatId]
export const researchApprovalAPI=async<T,>(chatId:string,suffix='',body?:unknown)=>answer<T>(await deskFetch('/api/input-research-approvals/'+encodeURIComponent(chatId)+suffix,{method:body===undefined?'GET':'POST',headers:{'Content-Type':'application/json'},...(body!==undefined?{body:JSON.stringify(body)}:{})}))
export function useResearchApproval(chatId:string){return useQuery({queryKey:approvalKey(chatId),queryFn:()=>researchApprovalAPI<ResearchApprovalStatus>(chatId),retry:false,staleTime:0,refetchInterval:5000})}
