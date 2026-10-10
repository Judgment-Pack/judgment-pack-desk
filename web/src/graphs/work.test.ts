import { expect, it } from 'vitest'
import { summarizeWork, readResponseHistory } from '../chat/responseHistory'
import { graphWork, graphWorkResult, validGraphWork } from './work'
it('retains rehearsal arguments and exact runtime text through history reload', () => {
  const inputs = '{"review":{"facts":{"amount":9007199254740993}}}', raw = '{ "rehearsal":true, "amount":9007199254740993 }\n'
  const work = summarizeWork([{type:'tool_call', name:'graph_rehearse', callId:'call-1', args:{id:'flow', inputs}}, {type:'tool_result', isError:false, name:'graph_rehearse', callId:'call-1', text:raw}], false)
  const history = readResponseHistory(JSON.parse(JSON.stringify([{id:'response-1', documents:[], websites:[], sourceIds:[], work}])))
  expect(history[0]?.work.items[0]).toMatchObject({name:'graph_rehearse', status:'complete', graph:{arguments:JSON.stringify({id:'flow',inputs}), result:raw}})
})
it('does not associate mismatched tool results with decision steps', () => {
  const work = summarizeWork([{type:'tool_call', name:'graph_rehearse', callId:'call-1', args:{}}, {type:'tool_result', isError:false, name:'read_link', callId:'call-1', text:'unrelated'}], false)
  expect(work.items[0]?.graph?.result).toBeUndefined()
})
it('marks bounded previews and rejects oversized saved previews', () => {
  const preview = graphWorkResult(graphWork({inputs:'x'.repeat(20000)}), 'r'.repeat(70000))
  expect(preview).toMatchObject({truncated:true}); expect(preview.arguments.length).toBe(16000); expect(preview.result?.length).toBe(65536)
  expect(validGraphWork(preview)).toBe(true); expect(validGraphWork({...preview,result:'r'.repeat(65537)})).toBe(false)
})
