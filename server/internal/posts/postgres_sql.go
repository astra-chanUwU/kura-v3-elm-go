package posts

// SearchPostsSQL is the handwritten query contract for the PostgreSQL
// adapter. The adapter will bind $1 to the user query and keep result shaping
// in Go. The schema and full-text index are introduced with the first DB
// migration, so no database driver is needed by this contract package yet.
const SearchPostsSQL = `
SELECT
    id::text,
    preview_url,
    original_url,
    media_type,
    width,
    height
FROM posts
WHERE deleted_at IS NULL
  AND search_document @@ websearch_to_tsquery('simple', $1)
ORDER BY id DESC
LIMIT 60
`
