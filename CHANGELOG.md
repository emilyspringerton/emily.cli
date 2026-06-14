## 2026-06-14
- feat(start): emily start --agi enables AGI loop mode; passes --continue to obs-watcher so claude RSI cycles build persistent context across invocations (the AGI loop pattern)

## 2026-06-13
- feat(backlog): `emily backlog add [--section N] "<item>"` — programmatic item insertion into any BACKLOG.md section (S22-08)
- feat(backlog): `emily backlog add-section [--title "<title>"]` — append new numbered section to BACKLOG.md (S22-08)
- feat(northstar): `emily northstar <repo>` — print NORTHSTAR.md for any repo; resolves docs/ and docs2/ (S22-09)

## 2026-06-12
- feat(context): emily context build — compresses 16 Tier 1 golden docs via haiku bilingual Chinese/English into EMILY/context/full-system-context.md

- emily tui: real token accounting from IDUNA Apple metadata (fetchTokenSpendFromIDUNA), F5 hotkey fires rsi-loop.sh iteration in background

