import {sourceMessage} from '../i18n/source'

// Closed Desk-owned reasons. Never persist raw provider errors or tool arguments.
export function dialectRecipeFailureMessage(code: unknown): string | undefined {
 switch(code) {
  case 'content': return sourceMessage('Use only name, intent, target, fields, criteria and limits in a recipe proposal. Connections and execution settings are chosen during review.')
  case 'name': return sourceMessage('Give the recipe a name between 1 and 160 characters.')
  case 'intent': return sourceMessage('Provide research instructions between 1 and 12,000 characters.')
  case 'target': return sourceMessage('Dialects research records. Describe what to find in the instructions and requested fields.')
  case 'fields': return sourceMessage('Provide 1–30 uniquely named fields. Each needs a description and type: string, number or boolean. Field names must start with a lowercase letter and contain only letters, numbers or underscores; constructor and prototype are reserved.')
  case 'criteria': return sourceMessage('Use up to 30 criteria with unique IDs and descriptions. Omit qualificationTarget when unused; otherwise provide a non-empty string of at most 512 characters.')
  case 'limits': return sourceMessage('Use integer limits: 1–50 records, 0–30 searches, 0–100 pages and 1–60 modelCalls.')
  case 'draft-limit': return sourceMessage('This chat has 16 dialect recipe drafts. Start a new chat to create another.')
  case 'chat-unavailable': return sourceMessage('This conversation is no longer available. Open a chat before proposing a recipe.')
  case 'proposal-unavailable': return sourceMessage('The recipe proposal could not be retained. Check chat history for a save error before trying again.')
 }
}
export type DialectRecipeFailureCode = 'content' | 'name' | 'intent' | 'target' | 'fields' | 'criteria' | 'limits' | 'draft-limit' | 'chat-unavailable' | 'proposal-unavailable'
export class DialectRecipeDraftError extends Error {
 constructor(readonly code: 'draft-limit' | 'chat-unavailable') { super(dialectRecipeFailureMessage(code)) }
}
