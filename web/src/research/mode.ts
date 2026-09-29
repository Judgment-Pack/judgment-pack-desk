/** `research` preserves the original source-led authoring/checkpoint contract.
 * New composer research uses the verified web tools and conversation lifecycle.
 */
export type AuthoringMode = 'draft' | 'web-research' | 'research'
export const conversationMode = (mode: AuthoringMode | undefined) => mode === 'draft' || mode === 'web-research'
