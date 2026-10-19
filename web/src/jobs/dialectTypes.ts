/** Desk names these documents Dialects; mapping remains the Runner wire member. */
import type { SourceInput, JobInput } from './client'
import type { RetainedJSON } from './wire'
import type { DialectDocumentV2, DialectDocumentV3, DialectSourceV3, InputProfile } from '../dialect/types'
export type { CalculatorPin, CalculationBinding, CalculationLineage, InputProfile, Parameter } from '../dialect/types'
export type { CopyMapping as DialectCopies, DialectSourceV3 as DialectSource, PreparationV2 as Preparation } from '../dialect/types'
/** Authoring values can be incomplete; Runner validates the source/version combination. */
export type ExecutableDialect = (Omit<DialectDocumentV2,'sources'> | Omit<DialectDocumentV3,'sources'> | (Omit<DialectDocumentV3,'sources'|'version'|'provider'|'facts'|'evidence'|'case'> & {formatVersion:'2.0.0'|'3.0.0';id:string;version:string;case:NonNullable<DialectDocumentV3['case']>})) & {sources?:DialectSourceV3[]}

export function isExecutableDialect(mapping: SourceInput['mapping'] | ExecutableDialect | undefined): mapping is ExecutableDialect {
 return !!mapping && (mapping.version===2 || mapping.version===3 || 'formatVersion' in mapping && (mapping.formatVersion==='2.0.0'||mapping.formatVersion==='3.0.0'))
}
/** An entry of Runner's listing: an installed profile, or (Runner v0.23.0) a
 * registered one Runner set aside when it opened its store, listed after the
 * installed ones with its state and reason. A set-aside profile's `profile`
 * is its stored bytes; it is kept and never bound. */
export interface ProfileEntry { profile: InputProfile; digest: string; state?: string | null; reason?: string }
export interface SourceV2 { import?:{dialect:ExecutableDialect;binding:unknown}; mapping: ExecutableDialect; case: Record<string, unknown>; sources: Record<string, {snapshot?: SourceInput['snapshot']; response?: RetainedJSON | unknown}>; mappingDigest?: string }
export interface InputPlan { next?: {name: string; kind: string; provider?: string; profile?: string; source?: string; arguments?: Record<string, unknown>}; input?: JobInput; factsText?: string; evidenceText?: string }
export const isSourceV2 = (s: SourceInput | SourceV2): s is SourceV2 => isExecutableDialect(s.mapping)
