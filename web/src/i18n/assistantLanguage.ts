import { LANGUAGES, type Language } from './locales'

/** A reply preference cannot change the runtime contract or source evidence. */
export function assistantLanguageInstructions(locale: Language = 'en'): string {
  const selected = LANGUAGES.find(item => item.id === locale) ?? LANGUAGES[0]
  return `\n\nRESPONSE LANGUAGE\nThe user's interface language is ${selected.instruction} (${selected.id}). Use that language for replies, clarifying questions, progress summaries, review explanations, and newly authored human-readable titles and descriptions, unless the user explicitly requests another language. Preserve existing authored content unless asked to translate it. Never translate tool names, JSON/schema keys, enumerated contract values, identifiers, fact paths, URLs, exact source quotations, or runtime output. Explain quoted output in the response language. Do not change decision semantics or test expectations when changing language.`
}
