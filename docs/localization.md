# Desk localization

The English catalogue and all 11 translated catalogues cover the current Desk UI messages. Translations were authored and reviewed directly, without sending UI text or project data to an external translation API. English remains the fallback for an unsupported system language or a translation bundle that cannot load. `npm --prefix web run i18n:check` rejects missing messages, invalid placeholders, and untranslated UI literals.

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
6. Keep deferred Desk notices in their canonical English form with the pure `sourceMessage` helper, and translate them with `systemMessage` when rendered. This preserves stored records and allows existing feedback to follow a language switch. Only explicitly named `message0`, `message1`, etc. interpolation slots contain nested Desk notices; ordinary interpolation values stay exact. Use `SourceError` for actionable Desk errors that `ErrorBox` should translate. Other errors remain raw diagnostics. Never mark source quotations, user content, or model prose as Desk notices.
7. Do not capture translated labels at module initialization or use them as document/grouping identifiers. Resolve shared vocabulary when rendering; keep stable keys independent of the language.
8. Run `npm --prefix web run i18n:extract`, translate the new catalogue entries, then run `npm --prefix web run i18n:check`. The checker validates coverage and interpolation/element placeholders.

Do not substitute English values into missing catalogue entries to make coverage pass. Identical translations are legitimate for names such as JSON, but an untranslated paragraph is unfinished work. Translation drafts require terminology and contextual review, especially Cantonese, Portuguese variants, and decision-contract explanations.

## Validation

The checks cover language negotiation, live switching with draft preservation, safe inline interpolation, CLDR plural forms, regional dates and numbers, and reply-language instructions for both the author and critic. Regression tests also cover stored feedback, interrupted replies, graph captions, inspector relationships, optional editor sections, offline recovery, and identifiers that happen to match translatable words.

CI runs catalogue validation and the UI-message guard alongside the component suite, type check and production build. The source guard checks literal JSX text, display and accessibility properties, option captions, confirmations, and captions inside `Message` slots. It cannot prove that every string passed through a variable is translated: imported labels and generated notices still require source and rendered review.

The browser audit exercises every language in an isolated project: home, Packs, tests, pack flows, Assistant settings, storage, identity settings, Help, and pack overview, logic and editing. It checks offline recovery, the configuration dialog, the language menu, draft preservation, reload persistence and visible untranslated labels. German, Cantonese and Japanese are also checked in both themes at 1440, 768 and 390 pixels. It does not send a message, call an AI provider or modify real chat data.

After building Desk, run it with Node 22 or later and a runtime fixture:

```sh
JPACK_BIN=/path/to/jpack node scripts/localization-check.mjs \
  /path/to/jpack-desk /path/to/runtime/internal/graph/testdata/project /tmp/localization-artifacts
```

The final repeated sweep found no remaining UI-copy gaps in the audited source and exercised states. This does not imply that user-authored packs, existing AI replies, exact source quotations or raw runtime/provider diagnostics change language; those intentionally retain their original content. Language quality can still benefit from native-speaker feedback in actual workflows.
