-- 为已有表补库内表/列注释，供管理工具阅读；不改列类型或已落库数据。

COMMENT ON TABLE factories IS 'WAN 工厂名录，不含厂内组织或普通账号';
COMMENT ON COLUMN factories.id IS '工厂稳定身份，也用来选厂库';
COMMENT ON COLUMN factories.name IS '工厂显示名，不当身份';
COMMENT ON COLUMN factories.created_at IS '名录入库时间';

COMMENT ON TABLE wan_admins IS 'WAN 唯一管理员；全表只能有一行';
COMMENT ON COLUMN wan_admins.id IS 'WAN 管理员稳定身份';
COMMENT ON COLUMN wan_admins.login_name IS 'WAN 登录名，全库唯一';
COMMENT ON COLUMN wan_admins.password_hash IS '日常密码哈希，只存在 WAN 库';
COMMENT ON COLUMN wan_admins.created_at IS '账号创建时间';

COMMENT ON TABLE initial_super_admins IS '交付对账用的初始超管身份，不含日常密码';
COMMENT ON COLUMN initial_super_admins.factory_id IS '一厂只能绑一名初始超管';
COMMENT ON COLUMN initial_super_admins.person_id IS '落在目标厂库里的账号身份';
COMMENT ON COLUMN initial_super_admins.login_name IS '交付时的登录名，不是秘密';
COMMENT ON COLUMN initial_super_admins.created_at IS '对账记录写入时间';

COMMENT ON TABLE sessions IS 'WAN 管理员会话；库里只存令牌哈希';
COMMENT ON COLUMN sessions.id IS '会话稳定身份';
COMMENT ON COLUMN sessions.admin_id IS '持有该会话的 WAN 管理员';
COMMENT ON COLUMN sessions.token_hash IS '会话令牌哈希，不存原文';
COMMENT ON COLUMN sessions.created_at IS '会话建立时间';
COMMENT ON COLUMN sessions.expires_at IS '过期后此会话立刻无效';

COMMENT ON TABLE audit_events IS 'WAN 审计：谁、对哪厂、做什么、允许还是拒绝';
COMMENT ON COLUMN audit_events.id IS '审计条目稳定身份';
COMMENT ON COLUMN audit_events.actor_id IS '操作者稳定身份；认不出则为空';
COMMENT ON COLUMN audit_events.claimed_login IS '被声明的登录标识';
COMMENT ON COLUMN audit_events.factory_id IS '相关工厂；无则空';
COMMENT ON COLUMN audit_events.org_unit_id IS '相关组织节点；WAN 侧通常为空';
COMMENT ON COLUMN audit_events.org_path IS '当时组织路径快照；无则空';
COMMENT ON COLUMN audit_events.action IS '做了什么操作';
COMMENT ON COLUMN audit_events.target IS '作用对象';
COMMENT ON COLUMN audit_events.result IS 'allow 或 deny';
COMMENT ON COLUMN audit_events.time_source IS '时间来源，本阶段固定 server';
COMMENT ON COLUMN audit_events.occurred_at IS '服务端记录时间';

COMMENT ON TABLE schema_migrations IS '已套用的向前迁移文件名，禁止改已落库数据';
COMMENT ON COLUMN schema_migrations.name IS '已执行的 SQL 文件名';
COMMENT ON COLUMN schema_migrations.applied_at IS '套用成功时间';
