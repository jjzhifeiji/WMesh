-- 为已有表补库内表/列注释，供管理工具阅读；不改列类型或已落库数据。

COMMENT ON TABLE people IS '本厂自然人账号，固定只属于本厂库，不属于任何组织节点';
COMMENT ON COLUMN people.id IS '稳定身份，改名也不变';
COMMENT ON COLUMN people.login_name IS '本厂内唯一登录名，不是身份';
COMMENT ON COLUMN people.display_name IS '显示名，可改';
COMMENT ON COLUMN people.status IS 'pending / active / disabled';
COMMENT ON COLUMN people.password_hash IS '日常密码哈希，只存在本厂；激活前为空';
COMMENT ON COLUMN people.activation_token_hash IS '一次性激活密码哈希，激活后清空';
COMMENT ON COLUMN people.is_initial_super_admin IS '本厂唯一的 WAN 下发初始超管';
COMMENT ON COLUMN people.created_at IS '账号创建时间';

COMMENT ON TABLE org_types IS '本厂自定义组织类型（场地、车间等），不预置层数';
COMMENT ON COLUMN org_types.id IS '组织类型稳定身份';
COMMENT ON COLUMN org_types.name IS '显示名，不当身份';
COMMENT ON COLUMN org_types.status IS 'active / disabled；有有效节点时不能停';
COMMENT ON COLUMN org_types.created_at IS '创建时间';

COMMENT ON TABLE org_units IS '本厂组织树上的节点，至多一个父节点，不能跨厂、不能成环';
COMMENT ON COLUMN org_units.id IS '节点稳定身份';
COMMENT ON COLUMN org_units.org_type_id IS '必须挂本厂已有组织类型';
COMMENT ON COLUMN org_units.parent_id IS '空表示直接挂在工厂下';
COMMENT ON COLUMN org_units.name IS '显示名，改名不改历史快照';
COMMENT ON COLUMN org_units.status IS '停用后不能再当新工作上下文';
COMMENT ON COLUMN org_units.created_at IS '创建时间';

COMMENT ON TABLE assignments IS '人员到组织节点的关系；取消只改状态，不删行';
COMMENT ON COLUMN assignments.id IS '分配关系稳定身份';
COMMENT ON COLUMN assignments.person_id IS '本厂人员';
COMMENT ON COLUMN assignments.org_unit_id IS '分配到的组织节点';
COMMENT ON COLUMN assignments.status IS 'active 或 ended；取消不删行';
COMMENT ON COLUMN assignments.created_at IS '分配开始时间';
COMMENT ON COLUMN assignments.ended_at IS '取消分配的时间；有效分配必须为空';

COMMENT ON TABLE role_grants IS '带明确作用域的一条角色；分配组织不会自动产生本行';
COMMENT ON COLUMN role_grants.id IS '授予记录稳定身份';
COMMENT ON COLUMN role_grants.person_id IS '被授予的本厂人员';
COMMENT ON COLUMN role_grants.role IS '六种固定角色之一';
COMMENT ON COLUMN role_grants.scope_kind IS 'factory 或 org_unit';
COMMENT ON COLUMN role_grants.org_unit_id IS 'Factory 作用域必须为空';
COMMENT ON COLUMN role_grants.status IS 'active 或 revoked';
COMMENT ON COLUMN role_grants.created_at IS '授予时间';
COMMENT ON COLUMN role_grants.revoked_at IS '收回时间；有效授予必须为空';

COMMENT ON TABLE sessions IS '厂内在线会话；库里只存令牌哈希';
COMMENT ON COLUMN sessions.id IS '会话稳定身份';
COMMENT ON COLUMN sessions.person_id IS '持有该会话的本厂人员';
COMMENT ON COLUMN sessions.token_hash IS '会话令牌哈希，不存原文';
COMMENT ON COLUMN sessions.created_at IS '会话建立时间';
COMMENT ON COLUMN sessions.expires_at IS '过期后立刻无效';

COMMENT ON TABLE audit_events IS '厂内审计：谁、对哪节点、做什么、允许还是拒绝';
COMMENT ON COLUMN audit_events.id IS '审计条目稳定身份';
COMMENT ON COLUMN audit_events.actor_id IS '操作者稳定身份；认不出则为空';
COMMENT ON COLUMN audit_events.claimed_login IS '被声明的登录标识';
COMMENT ON COLUMN audit_events.factory_id IS '所属工厂；本厂库写入时即本厂';
COMMENT ON COLUMN audit_events.org_unit_id IS '相关组织节点；无则空';
COMMENT ON COLUMN audit_events.org_path IS '当时组织路径快照；无则空';
COMMENT ON COLUMN audit_events.action IS '做了什么操作';
COMMENT ON COLUMN audit_events.target IS '作用对象';
COMMENT ON COLUMN audit_events.result IS 'allow 或 deny';
COMMENT ON COLUMN audit_events.time_source IS '时间来源，本阶段固定 server';
COMMENT ON COLUMN audit_events.occurred_at IS '服务端记录时间';

COMMENT ON TABLE fact_stubs IS '最小运行事实：只记创建人和发生时的组织路径';
COMMENT ON COLUMN fact_stubs.id IS '事实桩稳定身份';
COMMENT ON COLUMN fact_stubs.creator_id IS '创建账号稳定身份';
COMMENT ON COLUMN fact_stubs.factory_id IS '所属工厂';
COMMENT ON COLUMN fact_stubs.org_unit_id IS '发生节点；直属工厂时为空';
COMMENT ON COLUMN fact_stubs.org_path IS '当时从工厂到该节点的祖先快照；直属为空';
COMMENT ON COLUMN fact_stubs.created_at IS '发生时间；路径快照此后不得改写';

COMMENT ON TABLE personal_asset_stubs IS '最小个人级资产桩；内容不因超管身份打开';
COMMENT ON COLUMN personal_asset_stubs.id IS '个人资产桩稳定身份';
COMMENT ON COLUMN personal_asset_stubs.creator_id IS '创建人；仅本人可读';
COMMENT ON COLUMN personal_asset_stubs.factory_id IS '所属工厂';
COMMENT ON COLUMN personal_asset_stubs.org_unit_id IS '创建时节点；直属时为空';
COMMENT ON COLUMN personal_asset_stubs.org_path IS '创建时路径，改分配不改写';
COMMENT ON COLUMN personal_asset_stubs.content IS '内容正文；不进审计';
COMMENT ON COLUMN personal_asset_stubs.created_at IS '创建时间';

COMMENT ON TABLE schema_migrations IS '已套用的向前迁移文件名，禁止改已落库数据';
COMMENT ON COLUMN schema_migrations.name IS '已执行的 SQL 文件名';
COMMENT ON COLUMN schema_migrations.applied_at IS '套用成功时间';
