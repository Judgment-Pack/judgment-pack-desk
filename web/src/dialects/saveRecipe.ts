import {dialectAPI,type DialectRecipe,type PreparationReply} from './client'

/** Manual and chat review share the same checked compare-and-swap boundary.
 * Recipe saves never publish an artifact, run research or authorize a job. */
export function saveDialectRecipe(saved:PreparationReply,name:string,recipe:DialectRecipe) {
 return dialectAPI<PreparationReply>('/'+saved.document.id,'PUT',{name,recipe},saved.sha256)
}
