/** Desk names these documents Dialects; mapping remains the Runner wire member. */
import type { SourceInput, JobInput } from './client'
import type { RetainedJSON } from './wire'
import type { DialectDocumentV2, InputProfile } from '../dialect/types'
export type { CalculatorPin, CalculationBinding, CalculationLineage, InputProfile, Parameter } from '../dialect/types'
export type { CopyMapping as DialectCopies, DialectSourceV2 as DialectSource, PreparationV2 as Preparation } from '../dialect/types'
export type DialectV2 = DialectDocumentV2
export interface ProfileEntry { profile: InputProfile; digest: string }
export interface SourceV2 { mapping: DialectV2; case: Record<string, unknown>; sources: Record<string, {snapshot?: SourceInput['snapshot']; response?: RetainedJSON | unknown}>; mappingDigest?: string }
export interface InputPlan { next?: {name: string; kind: string; provider?: string; profile?: string; source?: string; arguments?: Record<string, unknown>}; input?: JobInput; factsText?: string; evidenceText?: string }
export const isSourceV2 = (s: SourceInput | SourceV2): s is SourceV2 => s.mapping.version === 2
