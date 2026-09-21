-- 本厂目录树：平台级副本 / 厂级 / 个人级各一棵；文件节点引用资产或副本身份。

CREATE TABLE asset_fs_nodes (
    id UUID PRIMARY KEY, -- 节点身份，不当路径
    name TEXT NOT NULL, -- 显示名；根必须空串
    parent_id UUID REFERENCES asset_fs_nodes (id), -- 空表示该树的根
    node_kind TEXT NOT NULL, -- folder 文件夹 / file 文件
    asset_kind TEXT NOT NULL, -- process / project，一棵树只一种
    tree_level TEXT NOT NULL, -- platform 已下发副本 / factory 厂级 / personal 个人级
    owner_id UUID REFERENCES people (id), -- 个人树主人；其余必须空
    asset_id UUID, -- 文件指向本厂资产或平台副本身份；文件夹必须空
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(), -- 创建时间
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(), -- 最近改名或搬家
    CHECK (node_kind IN ('folder', 'file')),
    CHECK (asset_kind IN ('process', 'project')),
    CHECK (tree_level IN ('platform', 'factory', 'personal')),
    CHECK (
        (tree_level = 'personal' AND owner_id IS NOT NULL)
        OR (tree_level <> 'personal' AND owner_id IS NULL)
    ),
    CHECK (
        (node_kind = 'folder' AND asset_id IS NULL)
        OR (node_kind = 'file' AND asset_id IS NOT NULL AND parent_id IS NOT NULL)
    ),
    CHECK (
        (parent_id IS NULL AND node_kind = 'folder' AND name = '')
        OR parent_id IS NOT NULL
    )
);

COMMENT ON TABLE asset_fs_nodes IS '本厂目录节点；路径不落库，顺着父节点走';
COMMENT ON COLUMN asset_fs_nodes.id IS '节点身份，不当路径';
COMMENT ON COLUMN asset_fs_nodes.name IS '显示名；根必须空串';
COMMENT ON COLUMN asset_fs_nodes.parent_id IS '空表示该树的根';
COMMENT ON COLUMN asset_fs_nodes.node_kind IS 'folder 文件夹 / file 文件';
COMMENT ON COLUMN asset_fs_nodes.asset_kind IS 'process / project，一棵树只一种';
COMMENT ON COLUMN asset_fs_nodes.tree_level IS 'platform 已下发副本 / factory 厂级 / personal 个人级';
COMMENT ON COLUMN asset_fs_nodes.owner_id IS '个人树主人；其余必须空';
COMMENT ON COLUMN asset_fs_nodes.asset_id IS '文件指向本厂资产或平台副本身份；文件夹必须空';
COMMENT ON COLUMN asset_fs_nodes.created_at IS '创建时间';
COMMENT ON COLUMN asset_fs_nodes.updated_at IS '最近改名或搬家';

CREATE UNIQUE INDEX asset_fs_roots
    ON asset_fs_nodes (asset_kind, tree_level, COALESCE(owner_id, '00000000-0000-0000-0000-000000000000'))
    WHERE parent_id IS NULL;

CREATE UNIQUE INDEX asset_fs_child_names
    ON asset_fs_nodes (parent_id, name)
    WHERE parent_id IS NOT NULL;

CREATE UNIQUE INDEX asset_fs_file_asset
    ON asset_fs_nodes (asset_id)
    WHERE node_kind = 'file';

CREATE INDEX asset_fs_parent_idx ON asset_fs_nodes (parent_id);

INSERT INTO asset_fs_nodes (id, name, parent_id, node_kind, asset_kind, tree_level, created_at, updated_at)
VALUES
    (gen_random_uuid(), '', NULL, 'folder', 'process', 'platform', now(), now()),
    (gen_random_uuid(), '', NULL, 'folder', 'project', 'platform', now(), now()),
    (gen_random_uuid(), '', NULL, 'folder', 'process', 'factory', now(), now()),
    (gen_random_uuid(), '', NULL, 'folder', 'project', 'factory', now(), now());

INSERT INTO asset_fs_nodes (id, name, parent_id, node_kind, asset_kind, tree_level, owner_id, created_at, updated_at)
SELECT gen_random_uuid(), '', NULL, 'folder', t.kind, 'personal', t.creator_id, now(), now()
FROM (SELECT DISTINCT kind, creator_id FROM assets WHERE level = 'personal') t;

INSERT INTO asset_fs_nodes (id, name, parent_id, node_kind, asset_kind, tree_level, owner_id, asset_id, created_at, updated_at)
SELECT gen_random_uuid(),
    CASE
        WHEN ROW_NUMBER() OVER (PARTITION BY a.level, a.kind, a.creator_id, a.name ORDER BY a.created_at, a.id) = 1 THEN a.name
        ELSE a.name || ' ' || a.code
    END,
    r.id,
    'file',
    a.kind,
    a.level,
    CASE WHEN a.level = 'personal' THEN a.creator_id ELSE NULL END,
    a.id,
    a.created_at,
    a.updated_at
FROM assets a
JOIN asset_fs_nodes r
    ON r.asset_kind = a.kind
    AND r.parent_id IS NULL
    AND r.tree_level = a.level
    AND (
        (a.level = 'personal' AND r.owner_id = a.creator_id)
        OR (a.level <> 'personal' AND r.owner_id IS NULL)
    )
WHERE a.level IN ('factory', 'personal');

INSERT INTO asset_fs_nodes (id, name, parent_id, node_kind, asset_kind, tree_level, asset_id, created_at, updated_at)
SELECT gen_random_uuid(),
    CASE
        WHEN ROW_NUMBER() OVER (PARTITION BY r.kind, COALESCE(r.name, '') ORDER BY r.received_at, r.id) = 1 THEN r.name
        ELSE r.name || ' ' || COALESCE(r.code, substr(r.id::text, 1, 8))
    END,
    root.id,
    'file',
    r.kind,
    'platform',
    r.id,
    r.received_at,
    r.received_at
FROM (
    SELECT DISTINCT ON (id) id, kind, name, code, received_at
    FROM asset_replicas
    WHERE retracted = false
    ORDER BY id, revision DESC
) r
JOIN asset_fs_nodes root ON root.asset_kind = r.kind AND root.tree_level = 'platform' AND root.parent_id IS NULL;
