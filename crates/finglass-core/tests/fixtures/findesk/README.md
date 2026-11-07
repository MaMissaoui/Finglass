# FinDesk fixture database

`findesk.db` was written by FinDesk's own code (Exposed + SQLite3MultipleCiphers, SQLCipher 4 defaults), from the
test in `FixtureGen.kt.txt`, run once in a throwaway FinDesk worktree. Passphrase: `fixture passphrase 1`. The
orphan rows at the end were inserted with foreign keys off, as older FinDesk databases may contain them.

Stored formats (checked with rusqlite):

- decimals (`amount`, `balance`, `balance_after`, `budget_limit`, `confidence`): REAL or INTEGER (NUMERIC affinity),
  e.g. `real:-0.1`, `int:100`, `real:-1234567.89`
- dates: TEXT `yyyy-MM-dd`; datetimes: TEXT `yyyy-MM-dd HH:mm:ss.SSS`, no zone
- booleans: INTEGER 0/1
- `journal_mode`: delete
- document `file_path`: absolute, as FinDesk stores it. After generation the three paths were rewritten to a neutral
  `/Users/example/.findesk/...` location (keeping the base names), so they never match the folder a test selects,
  which is the real-world case of a copied or moved FinDesk folder.

Tests copy this folder to a temp dir first and never write to it.
