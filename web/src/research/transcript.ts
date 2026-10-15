import type {RunState} from './run'

/** Saved messages stay exact. Only the model projection drops obsolete Desk
 * snapshots when an explicit authored statement separates them from user data.
 * Legacy mixed inputs remain intact; never guess where user material ends.
 */
export function modelTranscript(state:Pick<RunState,'turns'|'questions'>,excludeLatestUser=false):string {
 let latest=-1
 state.turns.forEach((turn,index)=>{if(turn.role==='user'&&turn.kind!=='note')latest=index})
 let omitted=false
 const lines=state.turns.flatMap((turn,index)=>{
  if(turn.kind==='note'||excludeLatestUser&&index===latest)return []
  const question=turn.kind==='question'?state.questions?.find(q=>q.id===turn.id):undefined
  if(question)return [`ASSISTANT QUESTION (suggestions, not a user decision): ${JSON.stringify(question.prompt)}`]
  let text=turn.text
  if(turn.role==='user'){
   if(index===latest)text=turn.input??turn.text
   else if(turn.statement!==undefined){text=turn.statement;omitted ||= turn.input!==undefined&&turn.input!==turn.statement}
   else text=turn.input??turn.text
  }
  return [`${turn.role==='user'?'PERSON':'ASSISTANT'}: ${text}`]
 })
 const note=omitted?'\nHistorical Desk workspace snapshots are omitted from this model context; authored messages and supplied material are retained. Original inputs remain saved in chat history. Use the current workspace context and tools to read current objects; ask if a past selection is needed.\n':''
 return `TRANSCRIPT SO FAR${note}\n${lines.join('\n\n')}`
}
