package posts

// SearchPostsSQL is the handwritten query contract for the PostgreSQL
// adapter. The adapter binds $1 to the user query and keeps result shaping in
// Go. The schema and full-text index are introduced by db/migrations.
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
