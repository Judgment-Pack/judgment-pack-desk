import type { Turn } from '../../research/run'
import type { TestSuite } from './model'
import { proposalForMessage } from './proposals'

/** Stable across retries, reloads and recovery from a draft into a saved pack. */
export async function testConversationTurns(owner: string, messages: NonNullable<TestSuite['messages']>, suite: TestSuite): Promise<Turn[]> {
  return Promise.all(messages.map(async (message, index) => {
    const proposalId = message.proposalId ?? (message.role === 'user' ? suite.proposals?.find(proposal=>proposal.at===message.at && proposal.request===message.text)?.id : proposalForMessage({...suite,messages},index)?.id)
    const bytes = new TextEncoder().encode(JSON.stringify([message.role,message.at,message.text,proposalId ?? '']))
    const digest = [...new Uint8Array(await crypto.subtle.digest('SHA-256',bytes))].map(byte=>byte.toString(16).padStart(2,'0')).join('')
    return {id:`tests-${digest}`,role:message.role,kind:'message',at:message.at,text:message.text,
      testDesign:{owner,...(proposalId?{proposalId}:{})}}
  }))
}
