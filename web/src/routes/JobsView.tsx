import { useMatch } from 'react-router-dom'
import { JobsContent, CreateJobContent } from '../jobs/JobsView'

/** Jobs share one full-width route shell; each screen supplies its own header and reading measure. */
export function JobsView() {
  const creating = useMatch('/jobs/new') !== null
  return <article className="detail" data-layout="page" data-measure="full">
    {creating ? <CreateJobContent /> : <JobsContent />}
  </article>
}
