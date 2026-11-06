import type {StoragePolicySnapshot} from './storageBudgetPolicy'

// Authored wire values, never a host/native recording or an admission grant.
export function storagePolicyFixture(): StoragePolicySnapshot {
  const limits = (bytes: string) => ({total: {bytes, files: '100', directories: '100'}, categories: Array.from({length: 9}, () => ({bytes, files: '100', directories: '100'}))})
  return {version: 1, installation: '00000000000000000000000000000001', sequence: '9007199254740993', revision: '9007199254740995', epoch: '2', workspace: {id: '00000000000000000000000000000002', generation: '1', limits: limits('1000')}, installationLimits: limits('9007199254740993'), defaultLimits: limits('2000'), observationMaxAgeNanos: '1000000000'}
}
