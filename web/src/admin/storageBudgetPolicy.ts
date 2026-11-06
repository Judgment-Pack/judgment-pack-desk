import {deskFetch} from '../files/client'
import {storageCategories, storageUInt, type StorageVector} from './storageBudgetStatus'

export const STORAGE_POLICY_MAX_BYTES = 16_384
export type StoragePolicyLimits = {total: StorageVector; categories: StorageVector[]}
export type StoragePolicyScope = 'installation' | 'default' | 'workspace'
export type StoragePolicySnapshot = {
  version: 1; installation: string; sequence: string; revision: string; epoch: string
  workspace: {id: string; generation: string; limits: StoragePolicyLimits}
  installationLimits: StoragePolicyLimits; defaultLimits: StoragePolicyLimits; observationMaxAgeNanos: string
}
export class StoragePolicyError extends Error {
  constructor(readonly kind: 'invalid' | 'conflict' | 'uncertain') {super('Storage policy request was not confirmed')}
}
const invalid = (): never => {throw new StoragePolicyError('invalid')}
function closed(value: unknown, keys: string[]): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return invalid()
  const v = value as Record<string, unknown>
  if (Object.keys(v).length !== keys.length || keys.some(k => !Object.prototype.hasOwnProperty.call(v, k))) return invalid()
  return v
}
function uint(value: unknown, maximum = 18446744073709551615n): string {
  try {const s = storageUInt(value); return BigInt(s) > 0n && BigInt(s) <= maximum ? s : invalid()} catch {return invalid()}
}
function id(value: unknown): string {return typeof value === 'string' && /^[a-f0-9]{32}$/.test(value) && !/^0+$/.test(value) ? value : invalid()}
function vector(value: unknown): StorageVector {
  const v = closed(value, ['bytes', 'files', 'directories'])
  return {bytes: uint(v.bytes, 1n << 60n), files: uint(v.files, 1n << 40n), directories: uint(v.directories, 1n << 40n)}
}
export function decodeStoragePolicyLimits(value: unknown): StoragePolicyLimits {
  const v = closed(value, ['total', 'categories'])
  if (!Array.isArray(v.categories) || v.categories.length !== storageCategories.length) return invalid()
  return {total: vector(v.total), categories: v.categories.map(vector)}
}
export function decodeStoragePolicySnapshot(value: unknown): StoragePolicySnapshot {
  const v = closed(value, ['version', 'installation', 'sequence', 'revision', 'epoch', 'workspace', 'installationLimits', 'defaultLimits', 'observationMaxAgeNanos'])
  const w = closed(v.workspace, ['id', 'generation', 'limits'])
  if (v.version !== 1) return invalid()
  return {version: 1, installation: id(v.installation), sequence: uint(v.sequence), revision: uint(v.revision), epoch: uint(v.epoch), workspace: {id: id(w.id), generation: uint(w.generation), limits: decodeStoragePolicyLimits(w.limits)}, installationLimits: decodeStoragePolicyLimits(v.installationLimits), defaultLimits: decodeStoragePolicyLimits(v.defaultLimits), observationMaxAgeNanos: uint(v.observationMaxAgeNanos, 9223372036854775807n)}
}
export function cloneStoragePolicyLimits(v: StoragePolicyLimits): StoragePolicyLimits {return {total: {...v.total}, categories: v.categories.map(row => ({...row}))}}
export function storagePolicyScopeLimits(v: StoragePolicySnapshot, scope: StoragePolicyScope): StoragePolicyLimits {
  return scope === 'installation' ? v.installationLimits : scope === 'default' ? v.defaultLimits : v.workspace.limits
}
async function checked(response: Response): Promise<Response> {
  if (!response.ok) {await response.body?.cancel(); throw new StoragePolicyError(response.status === 409 ? 'conflict' : response.status === 400 ? 'invalid' : 'uncertain')}
  return response
}
async function snapshot(response: Response): Promise<StoragePolicySnapshot> {
  await checked(response)
  if (!response.body) throw new StoragePolicyError('uncertain')
  const reader = response.body.getReader(), bytes = new Uint8Array(STORAGE_POLICY_MAX_BYTES)
  let length = 0
  try {
    for (;;) {
      const chunk = await reader.read()
      if (chunk.done) break
      if (chunk.value.byteLength > bytes.length - length) {await reader.cancel(); throw new StoragePolicyError('uncertain')}
      bytes.set(chunk.value, length); length += chunk.value.byteLength
    }
    return decodeStoragePolicySnapshot(JSON.parse(new TextDecoder('utf-8', {fatal: true}).decode(bytes.subarray(0, length))))
  } catch {throw new StoragePolicyError('uncertain')} finally {reader.releaseLock()}
}
// No retries, polling, inferred preparation or snapshot rebasing in this client.
export async function prepareStorageBudgetPolicy(): Promise<void> {
  const response = await checked(await deskFetch('/api/storage/policy/prepare', {method: 'POST', headers: {'Content-Type': 'application/json'}, body: '{}'}))
  if (response.status !== 204) {await response.body?.cancel(); throw new StoragePolicyError('uncertain')}
}
export async function readStorageBudgetPolicy(): Promise<StoragePolicySnapshot> {return snapshot(await deskFetch('/api/storage/policy'))}
export async function editStorageBudgetPolicy(expected: StoragePolicySnapshot, scope: StoragePolicyScope, limits: StoragePolicyLimits): Promise<StoragePolicySnapshot> {
  if (!['installation', 'default', 'workspace'].includes(scope)) return invalid()
  const body = JSON.stringify({expected: decodeStoragePolicySnapshot(expected), scope, limits: decodeStoragePolicyLimits(limits)})
  if (new TextEncoder().encode(body).byteLength > STORAGE_POLICY_MAX_BYTES) return invalid()
  const next = await snapshot(await deskFetch('/api/storage/policy', {method: 'POST', headers: {'Content-Type': 'application/json'}, body}))
  if (next.installation !== expected.installation || next.workspace.id !== expected.workspace.id || next.workspace.generation !== expected.workspace.generation) throw new StoragePolicyError('uncertain')
  return next
}
