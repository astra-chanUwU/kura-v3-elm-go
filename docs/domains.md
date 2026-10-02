# Kura V3 domain boundaries

The domain layer is the durable contract between Elm, the HTTP API, the CLI, and PostgreSQL. Each area should expose behavior through explicit types and commands rather than letting UI code reach into storage.

- **Posts and assets**: post identity, source metadata, asset variants, dimensions, and media references.
- **Tags**: canonical tags, aliases, implications, and edits to post-tag membership.
- **Collections**: ordered pools or collections of posts.
- **Revisions**: immutable revision records and revision changes. Reverting is a new revision with a link to the revision it reverses.
- **Moderation**: moderation actions and capability checks around sensitive operations.
- **Search**: lexer → parser → AST → validation → PostgreSQL query planning. The same query language serves Elm, the CLI, the API, agents, and saved searches.
- **Uploads and processing**: upload intent, object-storage references, and Go orchestration of libvips and ffmpeg/ffprobe where needed. Large media does not belong in PostgreSQL.
- **Jobs**: durable job records for work that must survive a process restart. Progress can initially be polled over ordinary HTTP.

The browser-facing API should return stable post summaries suited to `MediaGrid`; it should not expose database rows as an accidental UI contract. The `kura` CLI will use those same HTTP endpoints and machine-friendly flags such as `--json`, `--jsonl`, `--fields`, `--limit`, `--cursor`, `--dry-run`, and `--yes` as the API matures.
