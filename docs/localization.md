# Desk localization

All 5,696 current English messages are covered in the 11 non-English catalogues, including required CLDR plural forms. The v0.13 translation pass preserves every existing translation and completes the current development backlog; new messages introduced by the pending combined integration still need extraction and translation. Translations were authored and reviewed directly, without sending UI text or project data to an external translation API. English remains the fallback for missing messages, an unsupported system language or a translation bundle that cannot load. `npm --prefix web run i18n:check` rejects invalid placeholders, keys that `en.json` lacks, and untranslated UI literals, and warns about a message missing from a translated catalogue (English is shown); `--strict` makes that a failure, for the release's translation pull request.

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

The browser audit exercises every language in an isolated project: home, Library, Dialects and the new-dialect form, Jobs and job creation, all eight pack and graph sections, Assistant settings, storage, identity settings, Help, and pack editing. It checks the sign-in setup entry, the configuration dialog, the language menu, draft preservation, reload persistence and visible untranslated labels. German, Cantonese and Japanese are also checked in both themes at 1440, 768 and 390 pixels. It does not send a message, call an AI provider or modify real chat data.

After building Desk, run it with Node 22 or later and a runtime fixture:

```sh
JPACK_BIN=/path/to/jpack node scripts/localization-check.mjs \
  /path/to/jpack-desk /path/to/runtime/internal/graph/testdata/project /tmp/localization-artifacts
```

For targeted reruns, set `DESK_AUDIT_LANGUAGES` to a comma-separated list such as `de,ja`. Use a distinct `DESK_AUDIT_PORT` for each concurrent audit; the default is 8851. Each process creates its own fixture and configuration directories.

The v0.12 release sweep found no remaining UI-copy gaps in the then-audited source and exercised states. That historical result does not cover the later v0.13 additions. This does not imply that user-authored packs, existing AI replies, exact source quotations or raw runtime/provider diagnostics change language; those intentionally retain their original content. Language quality can still benefit from native-speaker feedback in actual workflows.

## v0.12.0 translation review

All 4,638 source messages are present in every translated catalogue, including required CLDR plural forms. The release review also compared values with English and reviewed 229 older test-workspace, source-refresh and navigation messages that had English values despite passing the presence check. Technical names, paths, interpolation-only messages and valid shared words remain unchanged. Overview and Brief have distinct tab labels; Usage describes references and use, rather than consumption volume.

Release verification includes strict catalogue/source checks and the browser sweep above. Translations were reviewed for terminology, regional wording and the distinction between historical observations, verified byte lineage and approval.

## Combined v0.13 catalog checkpoint — 2026-10-15

The [combined release scope](design/v013-release-scope.md) includes both Codex's
Desk changes and Claude's ongoing portable-Dialect integration. The initial
sequence was extraction/checking first, with translation deferred until integration.
The user subsequently requested translation of the current catalog immediately;
that completed pass is recorded below.

Extraction contains **5,696 English message keys**: 41 newly extracted keys and
7 obsolete keys removed from the previous English catalog. Five obsolete keys
were removed from each translated catalog, retaining every other translation
value unchanged and preserving applicable CLDR plural forms. Missing entries
were not filled with English text.

| Locales | Missing source keys per locale | Additional missing CLDR forms | Total pending per locale |
| --- | ---: | ---: | ---: |
| German, Japanese, Korean, Cantonese, simplified Chinese, traditional Chinese | 987 | 0 | 987 |
| Spanish, French, Italian, Brazilian Portuguese, European Portuguese | 987 | 13 | 1,000 |

At this extraction-only checkpoint, the normal catalog check permitted English
fallback and the strict check failed on the backlog. That historical result is
superseded by the completed translation pass below. After integration, extract
again, translate any new messages, review terminology in context, and repeat the
strict and representative layout checks.

Checkpoint validation passed the normal catalog/source check, all 56 localization
unit tests and the production frontend build. All 11 translated catalogs have
zero placeholder errors. The strict check was also run and failed only on the
missing translations listed above, as expected for this extraction-only stage.


## Combined v0.13 translation pass — 2026-10-15

At the user's request, translation proceeded against the current checkpoint
before Claude's separate integration. All **5,696 source messages** now have
translations in German, Spanish, French, Italian, Japanese, Korean, Brazilian
Portuguese, European Portuguese, Cantonese, simplified Chinese and traditional
Chinese. This adds **10,922 entries**: 987 new source messages per locale and 13
additional CLDR forms in each of Spanish, French, Italian and both Portuguese
catalogs. Every previously translated value is preserved.

The pass covers Dialect recipes and mappings, research review and recovery,
Jobs, shared AI context and memory, Plan mode, scoped approvals, proposal
correction, loading states, and local sign-in/storage messages. Terminology
review preserves the distinctions between proposed and saved work, evidence
review and verification, and permission and request limits. Schema keys, tool
names, enums, commands, paths and interpolation values remain exact. Numeric
limits and the few legitimate English-identical labels were reviewed separately.
The Portuguese variants and Cantonese were reviewed for regional wording.

Strict catalog/source validation passes for all 11 locales with zero placeholder
errors. All 56 localization tests and the production frontend build pass.
Catalog comparisons confirm that no existing translation was overwritten.
The isolated browser sweep passed in English and all 11 translated languages,
covering current routes, new-dialect setup, language switching, unsent draft
preservation and reload persistence. German, Cantonese and Japanese passed both
themes at 1440, 768 and 390 pixels. No page overflow, untranslated checked labels,
browser errors or saved conversations were reported.

Visual review also found that the shared chat-context button did not inherit
Desk’s font, leaving Korean glyphs unreadable on hosts without a Korean system
font. It now inherits the existing shared font stack; the browser audit checks
that the Korean fallback reaches that control. The final focused browser rerun
passed in Korean, European Portuguese and German after the font and wording
corrections, including German’s narrow layouts.

This completes translation of the current checkpoint, not the pending combined
release. Extract again after integration and translate any new messages before
running the final release gates.
