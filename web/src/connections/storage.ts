import { connectionCall, ConnectionRequestError } from './client'
import { sourceMessage } from '../i18n/source'
export const STORAGE_MAX_BYTES = 4 * 1024 * 1024
export interface StorageFile { context: string; id: string; name: string; kind: 'file'|'folder'; sizeBytes: number; revision: string; mediaType: string; editable: boolean; deletable: boolean }
export interface StoragePage { context: string; items: StorageFile[]; nextPageToken?: string; truncated: boolean; scope: string; searchMode: string }
export interface StorageRead { file: StorageFile; contentBase64: string }
export interface StoragePlan { id: string; action: 'create'|'update'|'delete'; target: string; name: string; revision: string; sizeBytes: number; state: 'prepared'|'completed'|'refused'|'needs-attention'; expires: string; confirmation: string; effect: 'write'|'trash'|'delete'; error?: string }
export function storageError(error: unknown): string {
 if (error instanceof ConnectionRequestError) {
  if (error.code === 'source-changed') return sourceMessage('This file changed. Reload it before making a new change.')
  if (error.code === 'conditional-write-unavailable') return sourceMessage('The provider did not supply a version lock. This file cannot be changed safely here.')
  if (error.code === 'confirmation-required') return sourceMessage('Type the exact file name to confirm deletion.')
  if (error.code === 'operation-uncertain') return sourceMessage('The result is uncertain. Check the file at its source before making another change.')
  if (error.code === 'permission-required') return sourceMessage('This connection does not have permission for this operation.')
  if (error.code === 'unsupported-file') return sourceMessage('This file cannot be edited here. Use its source application.')
  if (error.code === 'file-too-large') return sourceMessage('Choose a file no larger than 4 MiB.')
 }
 return error instanceof Error ? error.message : sourceMessage('Could not complete this request.')
}
export const storageCall = <T,>(provider: string, method: string, params: object = {}) => connectionCall<T>(method, params, undefined, provider)
export function encodeBytes(bytes: Uint8Array): string {
 if (bytes.length > STORAGE_MAX_BYTES) throw new Error(sourceMessage('Choose a file no larger than 4 MiB.'))
 let value = ''
 for (let i=0;i<bytes.length;i+=8192) value += String.fromCharCode(...bytes.subarray(i,i+8192))
 return btoa(value)
}
export function decodeText(value: string): string | undefined {
 try {
  const bytes = Uint8Array.from(atob(value), char => char.charCodeAt(0))
  const text = new TextDecoder('utf-8', {fatal:true}).decode(bytes)
  // Binary data is downloadable/uploadable, never coerced through a text editor.
  return /[\u0000-\u0008\u000B\u000C\u000E-\u001F]/.test(text) ? undefined : text
 } catch { return undefined }
}
