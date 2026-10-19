import { findLinks } from '../documents/link'
import { externalHref } from '../ui/ExternalLink'

export function websiteLinks(text: string) {
 return findLinks(text, true).flatMap(link => {
  const href = externalHref(link.text)
  return href ? [{...link, href}] : []
 })
}
interface Node {
 type: string; value?: string; url?: string; children?: Node[]
 position?: {start: {offset?: number}; end: {offset?: number}}
}
/** Work on prose nodes only: never rewrite code, HTML, image alt text, citation
 * labels, or existing Markdown destinations by searching the raw source. */
export function remarkWebsiteLinks() {
 return (tree: Node, file: {value: unknown}) => {
  const source = String(file.value)
  const visit = (node: Node) => {
   if (node.type === 'link') {
    // GFM defaults literal www addresses to HTTP; align these with bare domains.
    // An explicit [label](http://…) destination keeps its chosen scheme.
    const label = node.children?.length === 1 ? node.children[0]?.value : undefined
    if (label?.startsWith('www.') && node.url === `http://${label}` && source.slice(node.position?.start.offset, node.position?.end.offset) === label) node.url = `https://${label}`
    return
   }
   if (['code','inlineCode','linkReference','image','imageReference','html','definition'].includes(node.type) || !node.children) return
   node.children = node.children.flatMap(child => {
    if (child.type !== 'text' || !child.value) { visit(child); return [child] }
    const nodes: Node[] = []
    let offset = 0
    for (const link of websiteLinks(child.value)) {
     if (link.start > offset) nodes.push({type:'text', value:child.value.slice(offset, link.start)})
     nodes.push({type:'link',url:link.href,children:[{type:'text',value:link.text}]})
     offset = link.end
    }
    if (!nodes.length) return [child]
    if (offset < child.value.length) nodes.push({type:'text',value:child.value.slice(offset)})
    return nodes
   })
  }
  visit(tree)
 }
}
