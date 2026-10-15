// Shared with the existing Jobs tests so compatibility checks exercise their
// actual mapping fixtures, including incomplete drafts passed to the editor.
import type { DialectDocumentV2 } from '../types'

export const deskMappings = {
  'sequential': {version:2,sources:[{name:'first',kind:'operation',profile:'records',profileDigest:'sha256:pin',maxAge:300,arguments:{tool:'lookup',arguments:{}},read:{copy:{facts:[{target:'/id',source:'/id'}],evidence:[]}}},{name:'second',kind:'operation',profile:'records',profileDigest:'sha256:pin',maxAge:300,arguments:{tool:'lookup',arguments:{id:{$param:'id'}}},parameters:{id:{from:'first',pointer:'/id',type:'integer'}},read:{copy:{facts:[{target:'/name',source:'/name'}],evidence:[]}}}]},
  'drive-selection': {version:2,case:{parameters:{file:{pointer:'/picked/id',type:'string'},grant:{pointer:'/picked/grant',type:'string'}},facts:[],evidence:[]},sources:[{name:'drive',kind:'selected-file',provider:'google-drive',arguments:{fileId:{$param:'file'},grant:{$param:'grant'}},read:{copy:{facts:[],evidence:[]}}}]},
  'drive-reconnect': {version:2,case:{parameters:{file:{pointer:'/id',type:'string'},grant:{pointer:'/grant',type:'string'}},facts:[],evidence:[]},sources:[{name:'drive',kind:'selected-file',provider:'google-drive',profile:'records',profileDigest:'sha256:pin',arguments:{fileId:{$param:'file'},grant:{$param:'grant'}},read:{copy:{facts:[],evidence:[]}}}]},
  'integration-base': {version:2,case:{facts:[{target:'/manual',source:'/manual'}],evidence:[]},sources:[]},
  'copy-bindings': {version:2,case:{facts:[{target:'/score',source:'/facts/score'},{target:'/name',source:'/facts/name'}],evidence:[]},sources:[{name:'records',kind:'selected-file',provider:'local-file',read:{copy:{facts:[],evidence:[]}}}]},
  'case-score': {version:2,case:{facts:[{target:'/score',source:'/facts/score'}],evidence:[]},sources:[]},
  'editor-operation': {version:2,case:{facts:[],evidence:[]},sources:[{name:'records',kind:'operation',profile:'lookup',maxAge:300,arguments:{tool:'lookup',arguments:{}},read:{copy:{facts:[],evidence:[]}}}]},
  'editor-operation-2': {version:2,case:{facts:[],evidence:[]},sources:[{name:'records',kind:'operation',profile:'lookup',arguments:{tool:'lookup',arguments:{}},read:{copy:{facts:[],evidence:[]}}}]},
  'calculator-review':  {version:2,case:{facts:[],evidence:[]},sources:[{name:'conversion',kind:'operation',profile:'fx',read:{copy:{facts:[],evidence:[]}},calculation:{inputs:{amount:'invoiceAmount',currency:'invoiceCurrency'},tables:{'ecb-rates':86400,'spot-rates':3601}}}]},
  'automatic-sources': {version:2,case:{facts:[],evidence:[]},sources:[{name:'registry',kind:'operation',profile:'registry',read:{copy:{facts:[],evidence:[]}}},{name:'caseFile',kind:'selected-file',provider:'local-file',read:{copy:{facts:[],evidence:[]}}}]},
} satisfies Record<string, DialectDocumentV2>

export function mappingFixture(name: keyof typeof deskMappings): DialectDocumentV2 {
  return structuredClone(deskMappings[name])
}
