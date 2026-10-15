import {useQuery} from '@tanstack/react-query'
import {msg} from '../i18n'
import {dialectQuery} from './queries'
import {validDialectId} from './chatActions'
export function useDialectChatContext(id?:string,selection:string[]=[],tab='records',editing=false){
 const saved=useQuery({...dialectQuery(id??'list'),enabled:validDialectId(id),retry:false})
 if(!id)return undefined
 const doc=saved.data?.document,research=doc?.recipe.research
 const text=JSON.stringify({workspace:'Dialects',id,title:doc?.name??'Dialects',revision:saved.data?.sha256,recipeVersion:doc?.recipeVersion,tab,unsavedRecipeEdits:editing,selectedRecordIds:selection,recordCount:doc?.records.length,recipe:doc?{intent:doc.recipe.intent,research:research?{fields:research.fields,criteria:research.criteria,limits:research.limits}:undefined,qualification:doc.recipe.qualification?{kind:'pack',key:doc.recipe.qualification.key}:undefined}:undefined,instruction:'Use Desk Dialects tools for recipe design and record preparation. Keep total record counts and request budgets in structured limits; those controls take precedence over older totals in instructions. Read current recipe, records and activity when needed. Propose reviewed changes. Request runs with request_dialect_research; the server either admits a preapproved run or returns a review card. Report the actual result. Do not emit pack proposal fences or claim a proposal already changed anything. Keep research separate from qualification. Never change human review decisions. AI records need a current human Ready for evaluation review before pack evaluation. Never change that review yourself. No credentials or record values are included in this context.'})
 return {kind:'input' as const,originChatId:doc?.chatOrigin?.chatId,title:doc?.name??msg('Dialects'),detail:doc?msg('Recipe version {{version}}',{version:doc.recipeVersion})+(selection.length?' · '+msg('{{count}} selected',{count:selection.length}):''):msg('Prepare data for your decisions'),text,busy:id!=='list'&&(saved.isPending||saved.isError)}
}
