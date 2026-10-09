# Muse implementation slices

Run these one at a time from the repository root, after Luna's dataset import
has finished. Each run stops at a reviewable diff; supervising Codex reviews and
verifies the slice before the next. Do not run concurrent writers in this checkout.

| Order | Model / effort | Prompt |
| --- | --- | --- |
| 1 | Spark 1.2 / high | [01-write-gate-api.md](01-write-gate-api.md) |
| 2 | Spark 1.3 / high | [02-browser-writes.md](02-browser-writes.md) |
| 3 | Spark 1.2 / high | [03-collection-refresh.md](03-collection-refresh.md) |
| 4 | Spark 1.3 / high | [04-collection-pagination-api.md](04-collection-pagination-api.md) |
| 5 | Spark 1.3 / high | [05-collection-pagination-elm.md](05-collection-pagination-elm.md) |
| 6 | Spark 1.3 / high | [06-large-collection-ordering.md](06-large-collection-ordering.md) |

Use `muse exec --model muse-spark-1.2 --reasoning-effort high --workspace "$PWD"
--prompt-file docs/muse-slices/01-write-gate-api.md` (as one shell command).
Choose the model and prompt from the table for subsequent slices.

Test media, user paths, credentials, and local import manifests never belong
in the Git diff. Database integration tests require a separate disposable
database; never point them at the library used for daily browsing.
