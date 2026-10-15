import type { HostTool } from '../assistant/engine'
import type { Turn } from '../research/run'
import type { AuthoringMode } from '../research/mode'

export type ResearchChoice = 'ask' | 'research' | 'provided'
export function researchChoice(turn: Turn | undefined, mode: AuthoringMode | undefined, enabled: boolean): ResearchChoice {
 if (!enabled) return 'provided'
 return turn?.researchChoice ?? (mode === 'web-research' ? 'research' : 'ask')
}

/** Suggestions are local and conservative. The host gate covers requests this
 * heuristic misses, in any language, before a search or exploration executes. */
export function researchIntent(text: string): ResearchChoice | 'suggest' {
 const request = text.replace(/```[\s\S]*?```/g, '').replace(/^>.*$/gm, '').trim()
 if (/\b(?:do not|don't|without|no)\s+(?:web\s+)?(?:search|research|brows(?:e|ing))\b|\b(?:provided|attached|supplied) sources only\b/i.test(request)) return 'provided'
 if (/^(?:(?:please|can you|could you|would you)\s+)*(?:research\b|(?:do|conduct|perform)\s+(?:some |a |web )?research\b|search (?:the )?(?:web|online|internet)\b|browse (?:the )?(?:web|internet)\b)/i.test(request)) return 'research'
 if (/\b(?:latest|up.to.date|current (?:prices|pricing|news|trends|market)|competitors?|alternatives?|market (?:analysis|size|trends)|verify|fact.check)\b/i.test(request)
  || /\b(?:compare|find|recommend|investigate)\b.*\b(?:tools?|platforms?|companies|vendors?|providers?|products?|sources?|evidence|pricing|customers?|prospects?)\b/i.test(request)) return 'suggest'
 return 'ask'
}

/** The model may recommend Research; only a recorded user choice enables it. */
export function gateResearchTools(tools: HostTool[], choice: () => ResearchChoice): HostTool[] {
 return tools.map(tool => !['search_sources', 'explore_website'].includes(tool.name) ? tool : {...tool, execute: (args, signal) => {
  if (signal.aborted) return Promise.resolve({content:[{type:'text' as const,text:'The request was stopped.'}],isError:true})
  const policy=choice()
  if (policy === 'research') return tool.execute(args, signal)
  return Promise.resolve({content:[{type:'text' as const,text:policy === 'ask'
   ? 'Pause broader web research. Desk is asking the person to choose Use Research or Keep Chat for this request. No search or website exploration was performed. Briefly explain that a choice is needed, then stop. Do not repeat this call or use another tool to bypass the choice.'
   : 'For this request, use only the conversation and supplied sources. Do not search, explore websites, or request configuration as a workaround.'}], ...(policy === 'ask' ? {structuredContent:{researchSuggestion:true}} : {isError:true})})
 }})
}
