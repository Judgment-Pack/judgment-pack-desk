import type { ConnectionDescriptor } from '../connections/catalog'

// This provider exists only in tests. Its ID has no Desk handler or icon.
export const genericConnection: ConnectionDescriptor = {
 id: 'fixture-files', protocol: 'connection-v1', auth: 'credentials', registration: 'form', selection: 'source-search', queryRequired: false,
 operations: ['status','configure','search','select','disconnect'],
 presentation: {name:'Fixture files',icon:'',description:{en:'Selected fixture files',fr:'Fichiers de test sélectionnés'},instructions:{en:'Choose a fixture folder.',fr:'Choisissez un dossier de test.'}},
 setup: [{key:'folder',type:'text',label:{en:'Folder',fr:'Dossier'},required:true},{key:'key',type:'password',label:{en:'Access key',fr:'Clé d’accès'},required:true}],
 authorizationEndpoints: [], source: {id:'fixture-files',shape:'command',record:'resource-v1'},
}
export const genericCatalog = () => ({version:3,sources:[],providers:[structuredClone(genericConnection)]})
