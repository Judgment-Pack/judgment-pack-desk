import { msg } from '../i18n'
import { BriefPane } from './BriefPane'
import { useTestStorage } from '../packs/test-workspace/store'

/** A definition has one brief history across its tabs and attached chats. */
interface DefinitionBriefProps {
  kind: 'pack' | 'pack-draft' | 'graph' | 'graph-draft'; owner: string; title: string;
  content: string; sourceKey?: string; blocked?: string
}
export function DefinitionBrief(props: DefinitionBriefProps) {
  return props.kind === 'pack' || props.kind === 'pack-draft' ? <PackDefinitionBrief {...props}/> : <DefinitionPane {...props}/>
}
function PackDefinitionBrief(props: DefinitionBriefProps) {
  const { query } = useTestStorage(props.owner)
  return <DefinitionPane {...props} sourceKey={JSON.stringify([props.sourceKey ?? props.content, query.data?.sha256])}/>
}
function DefinitionPane({kind, owner, title, content, sourceKey = content, blocked}: DefinitionBriefProps) {
  const id = `${kind}:${owner}`
  return <BriefPane key={id} embedded subject={{ id, path: '/api/briefs?subject=' + encodeURIComponent(id), title }}
    content={content} sourceKey={sourceKey} generationUnavailable={blocked || (!content ? msg('Save a definition before generating its brief.') : undefined)} />
}
