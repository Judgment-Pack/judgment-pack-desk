/** Desk names these documents Dialects; mapping remains the Runner wire member. */
import type { SourceInput, JobInput } from './client'
import type { RetainedJSON } from './wire'
import type { DialectDocumentV2, InputProfile } from '../dialect/types'
export type { CalculatorPin, CalculationBinding, CalculationLineage, InputProfile, Parameter } from '../dialect/types'
export type { CopyMapping as DialectCopies, DialectSourceV2 as DialectSource, PreparationV2 as Preparation } from '../dialect/types'
export type ExecutableDialect = DialectDocumentV2 | (Omit<DialectDocumentV2,'version'|'provider'|'facts'|'evidence'> & {version:3}) | (Omit<DialectDocumentV2,'version'|'provider'|'facts'|'evidence'|'case'> & {formatVersion:'2.0.0'|'3.0.0';id:string;version:string;case:NonNullable<DialectDocumentV2['case']>})
export function isExecutableDialect(mapping: SourceInput['mapping'] | ExecutableDialect | undefined): mapping is ExecutableDialect {
 return !!mapping && (mapping.version===2 || mapping.version===3 || 'formatVersion' in mapping && (mapping.formatVersion==='2.0.0'||mapping.formatVersion==='3.0.0'))
}
export interface ProfileEntry { profile: InputProfile; digest: string }
export interface SourceV2 { mapping: ExecutableDialect; case: Record<string, unknown>; sources: Record<string, {snapshot?: SourceInput['snapshot']; response?: RetainedJSON | unknown}>; mappingDigest?: string }
export interface InputPlan { next?: {name: string; kind: string; provider?: string; profile?: string; source?: string; arguments?: Record<string, unknown>}; input?: JobInput; factsText?: string; evidenceText?: string }
export const isSourceV2 = (s: SourceInput | SourceV2): s is SourceV2 => isExecutableDialect(s.mapping)
