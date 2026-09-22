/** Gateway catalog v1 fixture; no account, endpoint, credential, or display copy. */
export const connectionCatalogFixture = { version: 1, providers: [
 { id: 'google-drive', auth: 'oauth', registration: 'google-desktop', selection: 'browser-picker', queryRequired: false, operations: ['status', 'configure', 'connect', 'pick', 'poll', 'cancel', 'disconnect'] },
 { id: 'gmail', auth: 'oauth', registration: 'google-desktop', selection: 'mail-search', queryRequired: false, operations: ['status', 'configure', 'connect', 'poll', 'cancel', 'disconnect', 'search', 'select'] },
 { id: 'notion', auth: 'oauth', registration: 'automatic', selection: 'source-search', queryRequired: true, operations: ['status', 'connect', 'poll', 'cancel', 'disconnect', 'search', 'select'] },
 { id: 'obsidian', auth: 'local-folder', registration: 'none', selection: 'source-search', queryRequired: false, operations: ['status', 'configure', 'disconnect', 'search', 'select'] },
] }
