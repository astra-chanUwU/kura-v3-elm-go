-- Deterministic local rows for exercising the search-to-MediaGrid slice.
-- The first three rows are API-only examples. The remaining rows point at
-- the twelve development fixtures under web/static/media/demo.
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
    (1003, '/media/demo/anime-preview.jpg', '/media/demo/anime-original.jpg', 'image/png', 512, 768, 'anime character illustration demo', '2026-01-01T00:00:03Z'),
    (2001, '/media/demo/new-folder-with-items-2.png', '/media/demo/new-folder-with-items-2.png', 'image/png', 627, 958, 'New Folder With Items 2 demo', '2026-01-02T00:00:01Z'),
    (2002, '/media/demo/new-folder-with-items.jpg', '/media/demo/new-folder-with-items.jpg', 'image/jpeg', 690, 976, 'New Folder With Items demo', '2026-01-02T00:00:02Z'),
    (2003, '/media/demo/revi.jpeg', '/media/demo/revi.jpeg', 'image/jpeg', 510, 679, 'レヴィ revi demo', '2026-01-02T00:00:03Z'),
    (2004, '/media/demo/kson.jpeg', '/media/demo/kson.jpeg', 'image/jpeg', 679, 437, 'kson demo', '2026-01-02T00:00:04Z'),
    (2005, '/media/demo/neon-paradox.jpeg', '/media/demo/neon-paradox.jpeg', 'image/jpeg', 680, 680, 'Neon Paradox demo', '2026-01-02T00:00:05Z'),
    (2006, '/media/demo/ruin-explorers.jpeg', '/media/demo/ruin-explorers.jpeg', 'image/jpeg', 1199, 836, 'Ruin Explorers demo', '2026-01-02T00:00:06Z'),
    (2007, '/media/demo/picopico256.png', '/media/demo/picopico256.png', 'image/png', 640, 360, 'picopico256 demo', '2026-01-02T00:00:07Z'),
    (2008, '/media/demo/pupi.jpeg', '/media/demo/pupi.jpeg', 'image/jpeg', 480, 680, 'Pupi 💫💥 demo', '2026-01-02T00:00:08Z'),
    (2009, '/media/demo/supernal.jpeg', '/media/demo/supernal.jpeg', 'image/jpeg', 381, 680, 'Supernal demo', '2026-01-02T00:00:09Z'),
    (2010, '/media/demo/ironlily-ordo-mediare-sisters.jpeg', '/media/demo/ironlily-ordo-mediare-sisters.jpeg', 'image/jpeg', 510, 680, 'Ironlily ⚖️Ordo Mediare Sisters demo', '2026-01-02T00:00:10Z'),
    (2011, '/media/demo/infinityark.jpeg', '/media/demo/infinityark.jpeg', 'image/jpeg', 413, 680, 'InfinityArk demo', '2026-01-02T00:00:11Z'),
    (2012, '/media/demo/liangmao.jpeg', '/media/demo/liangmao.jpeg', 'image/jpeg', 680, 680, '涼貓 demo', '2026-01-02T00:00:12Z')
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
