import type { Candidate } from '../research/run'
import { msg, useLocale } from '../i18n'
import { ArtifactReference } from './ArtifactReference'

export function DraftReference({
  candidate,
  open,
  latest,
  onOpen,
}: {
  candidate: Candidate
  open: boolean
  latest: boolean
  onOpen?: () => void
}) {
  useLocale()
  const title = (candidate.document as { title?: unknown })?.title
  return (
    <ArtifactReference
      revision={candidate.revision}
      title={typeof title === 'string' ? title : msg('Pack draft')}
      description={msg('Revision {{revision}}', { revision: candidate.revision })}
      indicator={open ? msg('Open') : undefined}
      action={latest ? msg('Open draft') : msg('Open latest draft')}
      onOpen={onOpen}
    />
  )
}
