import {wireJSON} from '../../chat/wire'
import type {SourceDeclaration,SourceReview,SourceSnapshot} from './catalog'

/** Display only. Use the shared wire formatter to retain every schema number token.
 * Its recursive traversal can exceed the JS call stack on deep schemas. Callers
 * must prepare the complete display before enabling decisions and fail closed on
 * any formatting error. Never substitute rounded, truncated or partial JSON.
 */
export function sourceReviewJSON(row:SourceReview):string {
 const declaration=(value:SourceDeclaration)=>'{'+Object.entries(value).map(([key,field])=>JSON.stringify(key)+':'+(key==='inputSchema'?wireJSON(value.inputSchema,true):JSON.stringify(field))).join(',')+'}'
 const snapshot=(value:SourceSnapshot)=>'{'+Object.entries(value).map(([key,field])=>JSON.stringify(key)+':'+(key==='completeDeclarations'?'['+value.completeDeclarations.map(declaration).join(',')+']':JSON.stringify(field))).join(',')+'}'
 return '{'+Object.entries(row).map(([key,value])=>JSON.stringify(key)+':'+(key==='snapshot'?snapshot(row.snapshot):JSON.stringify(value))).join(',')+'}'
}
