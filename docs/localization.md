# Desk localization

Localization is available for testing while the catalogues are being completed. The language selector does not imply complete translation coverage. `npm --prefix web run i18n:check` reports remaining catalogue gaps; missing entries continue to fall back to English.

## Language choice

The personal account menu offers **Language → Use system language** and an explicit language choice. The default follows `navigator.languages`, which normally reflects the operating system but can be overridden in browser preferences. The first supported preference wins; otherwise Desk uses English. An explicit choice is stored in this browser, outside project configuration. A browser that blocks storage can still change language for the current visit.

Supported choices are English, French, Spanish, German, Italian, European Portuguese, Brazilian Portuguese, Korean, simplified and traditional Mandarin Chinese, traditional Cantonese, and Japanese. `pt-BR` selects Brazilian Portuguese; other Portuguese regions select European Portuguese. A Chinese locale in Hong Kong selects traditional Chinese; Cantonese requires a `yue` language preference or an explicit choice. Region alone does not identify a person's spoken language.

Only the selected translation bundle is loaded, alongside the English fallback. Dates and numbers use `Intl`; following the system preserves regional conventions such as British English dates. Switching languages updates labels without remounting the workspace, changing the URL, clearing unsent messages, or resetting editors. Existing authored content remains verbatim.

Korean uses a self-hosted Noto Sans KR fallback where the operating system does not provide Hangul glyphs. Font files are served by Desk; the browser does not contact a font CDN.

## AI interaction

The engine-neutral assistant session carries an optional `replyLanguage`. The session runner supplies the current interface language when callers omit it. The Vercel adapter applies the preference to both the authoring/chat loop and the adversarial reviewer. An explicit language request in the conversation takes precedence. Existing messages are not rewritten when the interface language changes.

The preference covers replies, clarification questions, visible progress summaries, review explanations, and newly authored titles and descriptions. It does not translate tool names, JSON keys, contract enums, identifiers, fact paths, URLs, exact source quotations, or raw runtime results. Changing language must never change decision semantics or expected test results. Tool output and evidence remain inspectable exactly as received.

## Adding messages

1. Components displaying localized text call `useLocale()` and render `msg('An authored English message', { value })`. The subscription must not be used as a React `key`.
2. Translate complete sentences. Use named interpolation values instead of concatenating translated fragments. Use `Message` only when a sentence contains React elements or literal user content; numbered slots can move without changing their values.
3. Add count messages to `src/i18n/plurals.json` with their English singular form. Provide each locale's applicable CLDR forms in the catalogue. Never build a plural by appending English `s`.
4. Keep technical values outside translation functions, even if they resemble words. Display labels can differ from persisted keys and values. Do not pass pack text or assistant prose to `msg`.
5. Memoized projections containing translated labels must depend on the locale. Pure document parsing and editor identity must not depend on it.
6. Keep deferred Desk notices in their canonical English form with the dependency-free `sourceMessage` helper, and translate them with `systemMessage` when rendered. This preserves stored records and allows existing feedback to follow a language switch. Never mark source quotations, user content, or model prose as Desk notices.
7. Run `npm --prefix web run i18n:extract`, translate the new catalogue entries, then run `npm --prefix web run i18n:check`. The checker validates coverage and interpolation/element placeholders.

Do not substitute English values into missing catalogue entries to make coverage pass. Identical translations are legitimate for names such as JSON, but an untranslated paragraph is unfinished work. Translation drafts require terminology and contextual review, especially Cantonese, Portuguese variants, and decision-contract explanations.

## Validation and remaining work

The implementation includes language negotiation tests, live language switching and draft preservation tests, safe inline interpolation tests, plural/date/number tests, and model-boundary tests for author and critic requests. `scripts/localization-check.mjs` exercises all language choices in an isolated project, including narrow layouts and light/dark themes. It does not call an AI provider or modify real chat data.

Remaining before release:

- Complete all catalogues and review the terminology in context.
- Finish the audit of dynamic status/error text and English fragments in complex sentences. Preserve raw diagnostics as evidence while localizing surrounding controls and explanations.
- Check pack forms, test reports, graphs, storage dialogs, and error paths in translated layouts, including persistence during a live edit or assistant run.
- Re-run catalogue checks, component tests, browser checks, build, and the relevant mutation safeguards after the final copy changes.
