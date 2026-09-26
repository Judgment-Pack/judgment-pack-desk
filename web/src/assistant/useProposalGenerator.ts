import { useCallback, useEffect, useRef } from 'react'
import type { AssistantRun } from './useAssistantRun'
import type { ProposalReply } from './proposalWorkflow'
import { withAbort } from './engines/contract'

/** Adapt the existing engine event stream without giving a workflow another SDK. */
export function useProposalGenerator(run: AssistantRun) {
  const pending = useRef<((value: ProposalReply) => void) | null>(null)
  useEffect(() => {
    if (run.status !== 'finished' || !pending.current) return
    const resolve = pending.current
    pending.current = null
    const proposal = [...run.events].reverse().find((e) => e.type === 'proposal')
    const errors = run.events.filter((e) => e.type === 'error').map((e) => e.message)
    resolve({
      ...(proposal?.type === 'proposal'
        ? { document: proposal.document, unknowns: proposal.unknowns }
        : { unknowns: [] }),
      message: run.events
        .filter((e) => e.type === 'message')
        .map((e) => e.text)
        .join('\n'),
      failure: run.failure || errors.join('\n') || undefined,
    })
  }, [run.status, run.events, run.failure])
  return useCallback(
    async (prompt: string, signal: AbortSignal): Promise<ProposalReply> => {
      signal.throwIfAborted()
      const stop = () => run.stop()
      signal.addEventListener('abort', stop, { once: true })
      try {
        return await withAbort(
          () =>
            new Promise<ProposalReply>((resolve) => {
              pending.current = resolve
              run.start(prompt)
            }),
          signal,
        )
      } finally {
        pending.current = null
        signal.removeEventListener('abort', stop)
      }
    },
    [run.start, run.stop],
  )
}
