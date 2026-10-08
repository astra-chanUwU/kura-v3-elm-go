package posts

const listSavedSearchesSQL = `
SELECT id::text, name, query, created_at, updated_at
FROM saved_searches
WHERE owner_actor = $1
ORDER BY name ASC, id ASC
`

const insertSavedSearchSQL = `
INSERT INTO saved_searches (owner_actor, name, query)
VALUES ($1, $2, $3)
RETURNING id::text, name, query, created_at, updated_at
`

const updateSavedSearchSQL = `
UPDATE saved_searches
SET name = COALESCE($3::text, name),
    query = COALESCE($4::text, query),
    updated_at = now()
WHERE owner_actor = $1 AND id = $2::bigint
RETURNING id::text, name, query, created_at, updated_at
`

const deleteSavedSearchSQL = `
DELETE FROM saved_searches
WHERE owner_actor = $1 AND id = $2::bigint
`
