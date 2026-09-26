import { sourceMessage } from '../../i18n/source'

export const FOLDERS_FILE = 'jpack-folders.json'
export const HOME_FOLDER = 'home-local'
export const HOME_LABEL = 'local.user@example.com'
export const ALL_PACKS = 'all'
/** UI-only destination; cannot collide with a persisted folder identifier. */
export const WORKSPACE_ROOT = '__workspace__'
export const MAX_FOLDERS = 1000
export const MAX_DEPTH = 16
export interface PackFolder { id: string; name: string; parentId: string | null; ownerId?: 'local' }
export interface FolderDocument { version: 1; folders: PackFolder[]; assignments: Record<string, string> }
export type FolderAction =
 | { type: 'create'; id: string; name: string; parentId: string | null }
 | { type: 'rename'; id: string; name: string }
 | { type: 'move'; id: string; parentId: string | null }
 | { type: 'delete'; id: string }
 | { type: 'assign'; packId: string; folderId: string }
const identifier = (v: unknown): v is string => typeof v === 'string' && /^[a-zA-Z0-9-]{1,80}$/.test(v)
const object = (v: unknown): v is Record<string, unknown> => Boolean(v && typeof v === 'object' && !Array.isArray(v))
const only = (v: object, keys: string[]) => Object.keys(v).every(key => keys.includes(key))
export function defaultFolders(): FolderDocument { return { version: 1, folders: [{ id: HOME_FOLDER, name: HOME_LABEL, parentId: null, ownerId: 'local' }], assignments: {} } }
export function folderName(value: string): string {
 const name = value.trim().normalize('NFC')
 if (!name || [...name].length > 120 || /[\p{Cc}\p{Cf}/\\]/u.test(name) || name === '.' || name === '..') throw new Error(sourceMessage('Use a folder name of 1–120 characters without slashes or control characters.'))
 return name
}
export function folderTrail(document: FolderDocument, id: string): PackFolder[] {
 const trail: PackFolder[] = [], seen = new Set<string>()
 let current: string | null = id
 while (current !== null) {
  if (seen.has(current) || trail.length >= MAX_DEPTH) throw new Error(sourceMessage('Folders cannot contain themselves or exceed 16 levels.'))
  seen.add(current)
  const folder = document.folders.find(item => item.id === current)
  if (!folder) throw new Error(sourceMessage('This folder no longer exists. Choose another folder.'))
  trail.unshift(folder); current = folder.parentId
 }
 return trail
}
export function folderPath(document: FolderDocument, id: string) { return folderTrail(document, id).map(folder => folder.name).join(' / ') }
export function inFolder(document: FolderDocument, id: string, parent: string, recursive = false) {
 return parent === ALL_PACKS || id === parent || recursive && folderTrail(document, id).some(folder => folder.id === parent)
}
export function packFolder(document: FolderDocument, id: string) { return Object.hasOwn(document.assignments,id) ? document.assignments[id]! : HOME_FOLDER }
export function decodeFolders(value: unknown): FolderDocument {
 const invalid = () => { throw new Error(sourceMessage('Folder organization could not be read. The saved file has not been changed.')) }
 if (!object(value) || !only(value,['version','folders','assignments']) || value.version !== 1 || !Array.isArray(value.folders) || !value.folders.length || value.folders.length > MAX_FOLDERS || !object(value.assignments) || Object.keys(value.assignments).length > 10000) return invalid()
 const ids = new Set<string>(), names = new Set<string>()
 for (const folder of value.folders) {
  if (!object(folder) || !only(folder,['id','name','parentId','ownerId']) || !identifier(folder.id) || folder.id === ALL_PACKS || ids.has(folder.id) || typeof folder.name !== 'string' || folderName(folder.name) !== folder.name || folder.parentId !== null && !identifier(folder.parentId)) return invalid()
  if (folder.id === HOME_FOLDER ? folder.parentId !== null || folder.ownerId !== 'local' || folder.name !== HOME_LABEL : folder.ownerId !== undefined) return invalid()
  const nameKey = JSON.stringify([folder.parentId,folder.name.toLocaleLowerCase('en-US')])
  if (names.has(nameKey)) throw new Error(sourceMessage('A folder with this name already exists here.'))
  ids.add(folder.id); names.add(nameKey)
 }
 if (!ids.has(HOME_FOLDER)) return invalid()
 const doc = value as unknown as FolderDocument
 for (const folder of doc.folders) folderTrail(doc,folder.id)
 for (const [pack,folder] of Object.entries(doc.assignments)) if (!/^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$/.test(pack) || pack.length > 1024 || typeof folder !== 'string' || !ids.has(folder)) return invalid()
 return doc
}
export function applyFolderAction(document: FolderDocument, action: FolderAction): FolderDocument {
 const next: FolderDocument = { version: 1, folders: document.folders.map(folder => ({...folder})), assignments: {...document.assignments} }
 if (action.type === 'assign') {
  if (!next.folders.some(folder => folder.id === action.folderId)) throw new Error(sourceMessage('This folder no longer exists. Choose another folder.'))
  Object.defineProperty(next.assignments, action.packId, { value: action.folderId, enumerable: true, configurable: true, writable: true })
 } else if (action.type === 'create') next.folders.push({id:action.id,name:folderName(action.name),parentId:action.parentId})
 else {
  const folder = next.folders.find(folder => folder.id === action.id)
  if (!folder) throw new Error(sourceMessage('This folder no longer exists. Choose another folder.'))
  if (folder.id === HOME_FOLDER) throw new Error(sourceMessage('The home folder cannot be renamed, moved, or deleted.'))
  if (action.type === 'rename') folder.name = folderName(action.name)
  else if (action.type === 'move') folder.parentId = action.parentId
  else {
   if (next.folders.some(child => child.parentId === folder.id) || Object.values(next.assignments).includes(folder.id)) throw new Error(sourceMessage('Move the contents out before deleting this folder.'))
   next.folders = next.folders.filter(item => item.id !== folder.id)
  }
 }
 return decodeFolders(next)
}
