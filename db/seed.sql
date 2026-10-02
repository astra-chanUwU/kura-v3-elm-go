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
    source,
    artist,
    hash,
    file_size,
    tags,
    created_at
)
VALUES
    (1001, '/media/demo/cat-preview.jpg', '/media/demo/cat-original.jpg', 'image/jpeg', 640, 480, 'cat feline demo', '', '', '', 0, ARRAY['demo'], '2026-01-01T00:00:01Z'),
    (1002, '/media/demo/landscape-preview.jpg', '/media/demo/landscape-original.jpg', 'image/jpeg', 960, 640, 'landscape mountain outdoors demo', '', '', '', 0, ARRAY['demo'], '2026-01-01T00:00:02Z'),
    (1003, '/media/demo/anime-preview.jpg', '/media/demo/anime-original.jpg', 'image/png', 512, 768, 'anime character illustration demo', '', '', '', 0, ARRAY['demo'], '2026-01-01T00:00:03Z'),
    (2001, '/media/demo/new-folder-with-items-2.png', '/media/demo/new-folder-with-items-2.png', 'image/png', 627, 958, 'New Folder With Items 2 demo', 'demo/new-folder-with-items-2.png', 'Kura Demo', 'sha256:0000000000000000000000000000000000000000000000000000000000002001', 184321, ARRAY['demo', 'folder', 'illustration'], '2026-01-02T00:00:01Z'),
    (2002, '/media/demo/new-folder-with-items.jpg', '/media/demo/new-folder-with-items.jpg', 'image/jpeg', 690, 976, 'New Folder With Items demo', 'demo/new-folder-with-items.jpg', 'Kura Demo', 'sha256:0000000000000000000000000000000000000000000000000000000000002002', 192442, ARRAY['demo', 'folder', 'portrait'], '2026-01-02T00:00:02Z'),
    (2003, '/media/demo/revi.jpeg', '/media/demo/revi.jpeg', 'image/jpeg', 510, 679, 'レヴィ revi demo', 'demo/revi.jpeg', 'Kura Demo', 'sha256:0000000000000000000000000000000000000000000000000000000000002003', 173003, ARRAY['demo', 'revi', 'character'], '2026-01-02T00:00:03Z'),
    (2004, '/media/demo/kson.jpeg', '/media/demo/kson.jpeg', 'image/jpeg', 679, 437, 'kson demo', 'demo/kson.jpeg', 'Kura Demo', 'sha256:0000000000000000000000000000000000000000000000000000000000002004', 165554, ARRAY['demo', 'kson', 'portrait'], '2026-01-02T00:00:04Z'),
    (2005, '/media/demo/neon-paradox.jpeg', '/media/demo/neon-paradox.jpeg', 'image/jpeg', 680, 680, 'Neon Paradox demo', 'demo/neon-paradox.jpeg', 'Kura Demo', 'sha256:0000000000000000000000000000000000000000000000000000000000002005', 211005, ARRAY['demo', 'neon', 'square'], '2026-01-02T00:00:05Z'),
    (2006, '/media/demo/ruin-explorers.jpeg', '/media/demo/ruin-explorers.jpeg', 'image/jpeg', 1199, 836, 'Ruin Explorers demo', 'demo/ruin-explorers.jpeg', 'Kura Demo', 'sha256:0000000000000000000000000000000000000000000000000000000000002006', 243116, ARRAY['demo', 'ruin explorers', 'landscape'], '2026-01-02T00:00:06Z'),
    (2007, '/media/demo/picopico256.png', '/media/demo/picopico256.png', 'image/png', 640, 360, 'picopico256 demo', 'demo/picopico256.png', 'Kura Demo', 'sha256:0000000000000000000000000000000000000000000000000000000000002007', 128907, ARRAY['demo', 'pixel', 'animation'], '2026-01-02T00:00:07Z'),
    (2008, '/media/demo/pupi.jpeg', '/media/demo/pupi.jpeg', 'image/jpeg', 480, 680, 'Pupi 💫💥 demo', 'demo/pupi.jpeg', 'Kura Demo', 'sha256:0000000000000000000000000000000000000000000000000000000000002008', 174118, ARRAY['demo', 'pupi', 'character'], '2026-01-02T00:00:08Z'),
    (2009, '/media/demo/supernal.jpeg', '/media/demo/supernal.jpeg', 'image/jpeg', 381, 680, 'Supernal demo', 'demo/supernal.jpeg', 'Kura Demo', 'sha256:0000000000000000000000000000000000000000000000000000000000002009', 156229, ARRAY['demo', 'supernal', 'portrait'], '2026-01-02T00:00:09Z'),
    (2010, '/media/demo/ironlily-ordo-mediare-sisters.jpeg', '/media/demo/ironlily-ordo-mediare-sisters.jpeg', 'image/jpeg', 510, 680, 'Ironlily ⚖️Ordo Mediare Sisters demo', 'demo/ironlily-ordo-mediare-sisters.jpeg', 'Kura Demo', 'sha256:0000000000000000000000000000000000000000000000000000000000002010', 189330, ARRAY['demo', 'ironlily', 'sisters'], '2026-01-02T00:00:10Z'),
    (2011, '/media/demo/infinityark.jpeg', '/media/demo/infinityark.jpeg', 'image/jpeg', 413, 680, 'InfinityArk demo', 'demo/infinityark.jpeg', 'Kura Demo', 'sha256:0000000000000000000000000000000000000000000000000000000000002011', 161441, ARRAY['demo', 'infinityark', 'portrait'], '2026-01-02T00:00:11Z'),
    (2012, '/media/demo/liangmao.jpeg', '/media/demo/liangmao.jpeg', 'image/jpeg', 680, 680, '涼貓 demo', 'demo/liangmao.jpeg', 'Kura Demo', 'sha256:0000000000000000000000000000000000000000000000000000000000002012', 202552, ARRAY['demo', 'liangmao', 'cat'], '2026-01-02T00:00:12Z')
ON CONFLICT (id) DO UPDATE SET
    preview_url = EXCLUDED.preview_url,
    original_url = EXCLUDED.original_url,
    media_type = EXCLUDED.media_type,
    width = EXCLUDED.width,
    height = EXCLUDED.height,
    search_text = EXCLUDED.search_text,
    source = EXCLUDED.source,
    artist = EXCLUDED.artist,
    hash = EXCLUDED.hash,
    file_size = EXCLUDED.file_size,
    tags = EXCLUDED.tags,
    deleted_at = NULL,
    created_at = EXCLUDED.created_at;

SELECT setval(
    pg_get_serial_sequence('posts', 'id'),
    GREATEST(COALESCE((SELECT MAX(id) FROM posts), 1), 1),
    true
);
