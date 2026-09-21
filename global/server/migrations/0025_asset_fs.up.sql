-- 平台级工艺/工程目录树：文件夹单独成行，文件节点引用资产。

CREATE TABLE asset_fs_nodes (
    id UUID PRIMARY KEY, -- 节点身份，不当路径
    name TEXT NOT NULL, -- 显示名；根必须空串
    parent_id UUID REFERENCES asset_fs_nodes (id), -- 空表示该树的根
    node_kind TEXT NOT NULL, -- folder 文件夹 / file 文件
    asset_kind TEXT NOT NULL, -- process / project，一棵树只一种
    tree_level TEXT NOT NULL, -- 只允许 platform
    owner_id UUID, -- 平台树必须空
    asset_id UUID REFERENCES assets (id) ON DELETE CASCADE, -- 文件指向资产；文件夹必须空
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(), -- 创建时间
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(), -- 最近改名或搬家
    CHECK (node_kind IN ('folder', 'file')),
    CHECK (asset_kind IN ('process', 'project')),
    CHECK (tree_level = 'platform'),
    CHECK (owner_id IS NULL),
    CHECK (
        (node_kind = 'folder' AND asset_id IS NULL)
        OR (node_kind = 'file' AND asset_id IS NOT NULL AND parent_id IS NOT NULL)
    ),
    CHECK (
        (parent_id IS NULL AND node_kind = 'folder' AND name = '')
        OR parent_id IS NOT NULL
    )
);

COMMENT ON TABLE asset_fs_nodes IS '平台级目录节点；路径不落库，顺着父节点走';
COMMENT ON COLUMN asset_fs_nodes.id IS '节点身份，不当路径';
COMMENT ON COLUMN asset_fs_nodes.name IS '显示名；根必须空串';
COMMENT ON COLUMN asset_fs_nodes.parent_id IS '空表示该树的根';
COMMENT ON COLUMN asset_fs_nodes.node_kind IS 'folder 文件夹 / file 文件';
COMMENT ON COLUMN asset_fs_nodes.asset_kind IS 'process / project，一棵树只一种';
COMMENT ON COLUMN asset_fs_nodes.tree_level IS '只允许 platform';
COMMENT ON COLUMN asset_fs_nodes.owner_id IS '平台树必须空';
COMMENT ON COLUMN asset_fs_nodes.asset_id IS '文件指向资产；文件夹必须空';
COMMENT ON COLUMN asset_fs_nodes.created_at IS '创建时间';
COMMENT ON COLUMN asset_fs_nodes.updated_at IS '最近改名或搬家';

CREATE UNIQUE INDEX asset_fs_roots
    ON asset_fs_nodes (asset_kind)
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
    (gen_random_uuid(), '', NULL, 'folder', 'project', 'platform', now(), now());

INSERT INTO asset_fs_nodes (id, name, parent_id, node_kind, asset_kind, tree_level, asset_id, created_at, updated_at)
SELECT gen_random_uuid(),
    CASE
        WHEN ROW_NUMBER() OVER (PARTITION BY a.kind, a.name ORDER BY a.created_at, a.id) = 1 THEN a.name
        ELSE a.name || ' ' || a.code
    END,
    r.id,
    'file',
    a.kind,
    'platform',
    a.id,
    a.created_at,
    a.updated_at
FROM assets a
JOIN asset_fs_nodes r ON r.asset_kind = a.kind AND r.parent_id IS NULL;
