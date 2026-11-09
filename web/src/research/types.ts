import type {Ledger,SourceRecord} from './ledger'
import type {RunState,Turn} from './run'
import type {ReadPolicy} from './tools'

export interface DraftToolContext {
  recordSearch?: (reference: import('../search/results').SearchReference) => void
  recordDocument?: (file: import('../chat/store').ChatAttachment) => void
  recordWebsite?: (reference: import('../documents/website').WebsiteReference) => void
  turns: () => readonly Turn[]
  /** The turn's context policy, which holds a reading tool's window (`readWindowBytes`); the harness defaults where absent. */
  policy?: () => ReadPolicy
}

export interface ResearchRunBinding {
  run: import('./controller').ResearchRunController | null
  state: RunState
  ledger: Ledger | null
  sources: readonly SourceRecord[]
  /** Why a run cannot start, or '' where it can. */
  blocked: string
  /** The model this run would use, where one is enabled. */
  model: string
  researchConfigured: boolean
}
