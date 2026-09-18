/** Find authored UI literals which bypass the catalog. Document values and protocol data are not messages. */
import { parse } from '@babel/parser'
const props = new Set(['label', 'title', 'placeholder', 'aria-label', 'aria-description', 'aria-valuetext', 'ariaLabel', 'expectedLabel', 'expected', 'actualLabel', 'empty', 'emptyWhat', 'context', 'mapUnavailable', 'fallback', 'description', 'action', 'what', 'meta'])
const exact = new Set(['JSON', 'JPS', 'MCP', 'RFC', 'README', 'sha256', 'v', 'id', 'null', 'true', 'false', 'Google Gemini', 'Anthropic', 'op'])
export function untranslatedUiMessages(text) {
  const ast = parse(text, { sourceType: 'module', plugins: ['typescript', 'jsx'] })
  const issues = []
  function walk(node, ancestors = []) {
    const attribute = ancestors.findLast(p => p.type === 'JSXAttribute')
    const inMessage = ancestors.some(p => p.type === 'CallExpression' && ['msg', 'sourceMessage', 'systemMessage'].includes(p.callee.name))
      || (attribute?.name.name === 'text' && ancestors.some(p => p.type === 'JSXOpeningElement' && p.name.name === 'Message'))
    const rawContent = ancestors.some(p => p.type === 'JSXElement' && ['code', 'kbd', 'pre'].includes(p.openingElement.name.name))
    if (!inMessage && !rawContent && ['StringLiteral', 'JSXText', 'TemplateLiteral'].includes(node.type)) {
      const value = (node.type === 'TemplateLiteral' ? node.quasis.map(q => q.value.cooked).join('{{value}}') : node.value).trim()
      const parent = ancestors.at(-1)
      const inJsx = ancestors.some(p => p.type === 'JSXExpressionContainer')
      const property = ancestors.findLast(p => p.type === 'ObjectProperty')
      const isSlot = attribute?.name.name === 'slots' && ancestors.some(p => p.type === 'JSXOpeningElement' && p.name.name === 'Message')
      const isProp = attribute && props.has(attribute.name.name)
      const isOption = inJsx && property && props.has(property.key.name ?? property.key.value)
      const isChild = parent?.type === 'JSXExpressionContainer' && ancestors.at(-2)?.type === 'JSXElement'
      const confirmation = parent?.type === 'CallExpression' && parent.callee.type === 'MemberExpression' && parent.callee.object.name === 'window' && parent.callee.property.name === 'confirm'
      if ((node.type === 'JSXText' || isProp || isSlot || isOption || isChild || confirmation) && /[A-Za-z]/.test(value.replaceAll('{{value}}', '')) && !exact.has(value) && !/^v\{\{value\}\}$/.test(value)) {
        // Literal unions/comparison operands are control flow, not displayed text.
        const control = ancestors.slice(ancestors.findLastIndex(p => p.type === 'JSXExpressionContainer') + 1).some(p => p.type === 'BinaryExpression' && p.operator !== '+')
        const indexed = parent?.type === 'MemberExpression' && parent.property === node
        const fieldLabel = ancestors.some(p => p.type === 'CallExpression' && p.callee.name === 'valueLabel')
        if (!control && !indexed && !fieldLabel) issues.push({ line: node.loc.start.line, text: value })
      }
    }
    for (const [key, value] of Object.entries(node)) {
      if (['loc', 'comments', 'leadingComments', 'trailingComments', 'innerComments'].includes(key)) continue
      for (const child of Array.isArray(value) ? value : [value]) if (child?.type) walk(child, [...ancestors, node])
    }
  }
  walk(ast)
  return issues
}
