import type { InputMapping, SourceInput, JobInput } from './client'
import type { RetainedJSON } from './wire'
export interface InputProfile {
 id: string; publicKey: string; class: 'record' | 'generated'; source: string; authority: string; shape: 'mcp' | 'http' | 'command';
 adapter: {name: string; version: string; digest: string}; endpoint: string | null; tools?: string[];
}
export interface ProfileEntry { profile: InputProfile; digest: string }
export interface Parameter { from?: string; pointer: string; type: 'integer' | 'string' | 'timestamp' }
export interface CopyMapping { facts: InputMapping['facts']; evidence: InputMapping['evidence'] }
export interface MappingSource {
 name: string; kind: 'operation' | 'selected-file'; provider?: InputMapping['provider']; profile?: string; profileDigest?: string; maxAge?: number;
 parameters?: Record<string, Parameter>; arguments?: Record<string, unknown>;
 read: { unwrap?: string[]; copy?: CopyMapping; rule?: Record<string, unknown> };
}
export interface MappingV2 {
 version: 2; case?: CopyMapping & { parameters?: Record<string, Parameter> }; sources?: MappingSource[];
 admits?: { facts?: Record<string, string[]>; evidence?: Record<string, string[]> }; unmapped?: string[]; unmappedEvidence?: string[];
}
export interface SourceV2 { mapping: MappingV2; case: Record<string, unknown>; sources: Record<string, {snapshot?: SourceInput['snapshot']; response?: RetainedJSON | unknown}>; mappingDigest?: string }
export interface Preparation {
 version: 2; verifiedAt: string; mappingDigest: string; verification: string;
 lineage: {target: string; kind: string; source: string; class: string; generatedInfluence: boolean; present: boolean; status: string; reason: string; receipt?: unknown}[];
 cites: unknown[]; outcomes: {name: string; status: string; reason: string}[];
}
export interface InputPlan { next?: {name: string; kind: string; provider?: string; profile?: string; source?: string; arguments?: Record<string, unknown>}; input?: JobInput; factsText?: string; evidenceText?: string }
export const isSourceV2 = (s: SourceInput | SourceV2): s is SourceV2 => s.mapping.version === 2
