import type { DecisionMeaning } from '../packs/decisionAppearance'
export function DecisionSymbol({ meaning = 'categorical' }: { meaning?: DecisionMeaning }) {
  return <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
    {meaning === 'proceed' ? <><circle cx="8" cy="8" r="6"/><path d="m5 8 2 2 4-4"/></> : meaning === 'hold' ? <><circle cx="8" cy="8" r="6"/><path d="M6 5.5v5m4-5v5"/></> : meaning === 'handoff' ? <path d="M2 8h12m-5-5 5 5-5 5"/> : meaning === 'review' ? <><circle cx="6.5" cy="6.5" r="4.5"/><path d="m10 10 4 4"/></> : <circle cx="8" cy="8" r="5"/>}
  </svg>
}
