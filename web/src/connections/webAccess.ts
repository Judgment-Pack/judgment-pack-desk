import {privateQuery} from '../data/privateQuery'
import { useQuery } from '@tanstack/react-query'
import { answer, deskFetch } from '../files/client'
export interface WebAccess { supported: boolean; enabled: boolean; managed: boolean; local: boolean; revision: string }
export const WEB_ACCESS_KEY = ['web-access'] as const
export const useWebAccess = () => useQuery({...privateQuery,queryKey: WEB_ACCESS_KEY, queryFn: async ({signal}) => answer<WebAccess>(await deskFetch('/api/web-access', {signal})), retry:false})
export async function applyWebAccess(enabled:boolean, revision:string):Promise<WebAccess> {
 return answer<WebAccess>(await deskFetch('/api/web-access', {method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({enabled,revision,restartWarned:true})}))
}
