import { Disclosure } from '../ui/Disclosure'
import { ButtonLink } from '../ui/Button'
import { useSearchParams } from 'react-router-dom'
import type { ReactNode } from 'react'
import { msg } from '../i18n'
import { useTestStorage } from '../packs/test-workspace/store'
import { jsonIdentity } from '../research/checkCandidate'
import styles from './BriefPane.module.css'

export function PackReview({ owner, content, validation, findings, attention = false }: {
  owner: string; title: string; content: string; validation: string; findings: ReactNode; attention?: boolean
}) {
  const [params]=useSearchParams()
  const testHref='/packs/'+encodeURIComponent(owner)+'/tests'+(params.get('chat')?'?chat='+encodeURIComponent(params.get('chat')!):'')
  const { suite, query } = useTestStorage(owner)
  const run = suite.runs.at(-1)
  const matching = run && run.packText === content && jsonIdentity(run.cases) === jsonIdentity(suite.cases)
  return <div className={styles.review}>
    <section aria-label={msg('Readiness')}><h2>{msg('Readiness')}</h2>
      <dl className={styles.readiness}>
        <dt>{msg('Validation')}</dt><dd>{validation}</dd>
        <dt>{msg('Saved test cases')}</dt><dd>{query.isSuccess ? suite.cases.length : msg('Unavailable')}</dd>
        <dt>{msg('Last test run')}</dt><dd>{!run ? msg('Not recorded') : matching ? run.error ? msg('Run failed') : run.report?.status ? msg('{{status}} · recorded for this dialect',{status:run.report.status}) : msg('Recorded for this dialect') : msg('Dialects have changed')}</dd>
      </dl>
      <p className={styles.meta}>{msg('Review validation and test findings before using this pack. A brief does not establish readiness.')}</p>
    </section>
    <Disclosure title={msg('Validation findings')} open={attention}>{findings}</Disclosure>
    <ButtonLink variant="quiet" to={testHref}>{msg('Review tests')}</ButtonLink>
  </div>
}
