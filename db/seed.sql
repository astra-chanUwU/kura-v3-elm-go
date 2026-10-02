-- Deterministic local rows for exercising the search-to-MediaGrid slice.
-- The paths intentionally point at local media placeholders; no external
-- service or asset download is required.
INSERT INTO posts (
    id,
    preview_url,
    original_url,
    media_type,
    width,
    height,
    search_text,
    created_at
)
VALUES
    (1001, '/media/demo/cat-preview.jpg', '/media/demo/cat-original.jpg', 'image/jpeg', 640, 480, 'cat feline demo', '2026-01-01T00:00:01Z'),
    (1002, '/media/demo/landscape-preview.jpg', '/media/demo/landscape-original.jpg', 'image/jpeg', 960, 640, 'landscape mountain outdoors demo', '2026-01-01T00:00:02Z'),
    (1003, '/media/demo/anime-preview.jpg', '/media/demo/anime-original.jpg', 'image/png', 512, 768, 'anime character illustration demo', '2026-01-01T00:00:03Z')
ON CONFLICT (id) DO UPDATE SET
    preview_url = EXCLUDED.preview_url,
    original_url = EXCLUDED.original_url,
    media_type = EXCLUDED.media_type,
    width = EXCLUDED.width,
    height = EXCLUDED.height,
    search_text = EXCLUDED.search_text,
    deleted_at = NULL,
    created_at = EXCLUDED.created_at;

SELECT setval(
    pg_get_serial_sequence('posts', 'id'),
    GREATEST(COALESCE((SELECT MAX(id) FROM posts), 1), 1),
    true
);
