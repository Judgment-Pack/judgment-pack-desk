import type { ConnectionStatus, SourceSearch } from './client'
import { sourceMessage } from '../i18n/source'
import { validWebURL } from '../documents/record'

export const RESOURCE_MAX_BYTES = 4 << 20
const encode = new TextEncoder()
const text = (value: unknown, max: number, empty = false): value is string => typeof value === 'string' && (empty || value.length > 0) && encode.encode(value).length <= max && !/[\x00-\x1f\x7f-\x9f]/.test(value)
const object = (value: unknown): value is Record<string, unknown> => Boolean(value && typeof value === 'object' && !Array.isArray(value))
function invalid(): never { throw new Error(sourceMessage('The connection returned an invalid response. Try again.')) }

export function readResourceStatus(value: unknown, provider: string): ConnectionStatus {
 if (!object(value) || value.version !== 1 || value.provider !== provider || !['setup-required','not-connected','connected','blocked','unavailable'].includes(value.state as string) || !Number.isSafeInteger(value.maxFiles) || (value.maxFiles as number)<1 || (value.maxFiles as number)>4 || !Number.isSafeInteger(value.maxFileBytes) || (value.maxFileBytes as number)<1 || (value.maxFileBytes as number)>RESOURCE_MAX_BYTES) invalid()
 if (value.resource !== undefined && (!object(value.resource) || !text(value.resource.id,1024) || !text(value.resource.name,1024))) invalid()
 if (value.account !== undefined && (!object(value.account) || !text(value.account.id,1024) || value.account.name !== undefined && !text(value.account.name,1024,true) || value.account.email !== undefined && !text(value.account.email,1024,true))) invalid()
 return value as unknown as ConnectionStatus
}

export function readResourcePage(value: unknown): SourceSearch {
 if (!object(value) || !text(value.selectionContext,256) || !Array.isArray(value.items) || value.items.length>50 || typeof value.more !== 'boolean' || value.more !== Boolean(value.nextPageToken) || value.nextPageToken !== undefined && !text(value.nextPageToken,4096) || encode.encode(JSON.stringify(value)).length>48<<10) invalid()
 const ids=new Set<string>()
 for(const item of value.items) {
  if (!object(item) || !text(item.id,4096) || ids.has(item.id) || !text(item.title,1024) || item.description !== undefined && !text(item.description,2048,true) || !(item.url === '' || validWebURL(item.url) && !(item.url as string).includes('?')) || item.sizeBytes !== undefined && (!Number.isSafeInteger(item.sizeBytes) || (item.sizeBytes as number)<0) || item.unavailableReason !== undefined && !text(item.unavailableReason,64)) invalid()
  ids.add(item.id)
 }
 return value as unknown as SourceSearch
}
