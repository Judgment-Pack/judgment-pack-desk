import type {DialectChatAction} from '../dialects/chatActions'
import type {DialectPublicationAnswer} from '../dialects/DialectPublication'
export const publicationId='prep_'+'b'.repeat(32),publicationRevision='a'.repeat(64)
export function publicationAction():DialectChatAction {
 return {version:1,id:'publication-one',createdAt:'2026-10-16T12:00:00Z',inputId:publicationId,name:'Records',revision:publicationRevision,recipeVersion:1,kind:'publish-dialect',publication:{variant:'asserted',version:'1.0.0',impact:{classification:'initial',rationale:'First reviewed mapping'},answer:{publication:{id:'c'.repeat(64),at:'2026-10-16T12:00:00Z',dialect:{preparationId:publicationId,revision:publicationRevision,variant:'asserted',request:'d'.repeat(64)},documents:[{id:`urn:judgmentpack:dialect:${publicationId}:asserted`,version:'1.0.0'}]},document:'{"version":"1.0.0"}',digest:'sha256:'+'e'.repeat(64),saved:false,basePublication:'',review:{sha256:'f'.repeat(64),bytes:100}}}}
}
export function publishedAnswer(action=publicationAction()):DialectPublicationAnswer{return {...action.publication!.answer,saved:true}}
