import { FileRequestError, readFile, writeFile } from '../../files/client'
import { sourceMessage } from '../../i18n/source'
import { applyFolderAction, decodeFolders, defaultFolders, FOLDERS_FILE, type FolderAction, type FolderDocument } from './model'
export interface FolderSnapshot { document: FolderDocument; digest: string }
export const FOLDERS_KEY = ['pack-folders'] as const
export async function loadFolders(signal?: AbortSignal): Promise<FolderSnapshot> {
 try {
  const file = await readFile(FOLDERS_FILE,signal)
  if (file.bytes > 2_000_000) throw new Error(sourceMessage('Folder organization could not be read. The saved file has not been changed.'))
  let value: unknown
  try { value = JSON.parse(file.content) } catch { throw new Error(sourceMessage('Folder organization could not be read. The saved file has not been changed.')) }
  return { document: decodeFolders(value), digest: file.sha256 }
 } catch (cause) {
  if (cause instanceof FileRequestError && cause.status === 404 && cause.code === 'not-found') return { document: defaultFolders(), digest: '' }
  throw cause
 }
}
export async function saveFolderAction(before: FolderSnapshot, action: FolderAction): Promise<FolderSnapshot> {
 const document = applyFolderAction(before.document, action)
 const content=JSON.stringify(document,null,2)+'\n'
 if(new TextEncoder().encode(content).length>2_000_000)throw new Error(sourceMessage('Folder organization has reached its storage limit.'))
 try {
  const saved = await writeFile({path:FOLDERS_FILE,content,baseSha256:before.digest})
  return {document:decodeFolders(JSON.parse(saved.content)),digest:saved.sha256}
 } catch (cause) {
  if (cause instanceof FileRequestError && cause.status === 409) throw new Error(sourceMessage('Folders changed in another window. Reload folders and try again.'))
  throw cause
 }
}
export async function assignCreatedPack(packId: string, folderId: string) {
 const current = await loadFolders()
 return saveFolderAction(current,{type:'assign',packId,folderId})
}
