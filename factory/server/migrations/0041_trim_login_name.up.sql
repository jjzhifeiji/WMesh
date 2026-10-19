-- 登录名去掉首尾空白，网页和示教器按同一名字认人；空白集合与 Go 的 strings.TrimSpace 一致。
-- 去掉后为空，或与别的账号去掉后同名（含两条带空白的互撞），都保持原样，不合并、不撞唯一约束。
-- 密码哈希不动；还在用默认密码的，需超管按新名字重置一次。可重复执行，已去过的不再改。
-- +goose Up

WITH norm AS (
    SELECT id,
           login_name,
           regexp_replace(
               login_name,
               '^[\u0020\t\n\v\f\r\u0085\u00A0\u1680\u2000-\u200A\u2028\u2029\u202F\u205F\u3000]+|[\u0020\t\n\v\f\r\u0085\u00A0\u1680\u2000-\u200A\u2028\u2029\u202F\u205F\u3000]+$',
               '',
               'g'
           ) AS trimmed -- 去掉首尾空白后的登录名
    FROM people
),
movable AS (
    SELECT n.id, n.trimmed
    FROM norm AS n
    WHERE n.trimmed <> n.login_name -- 确有首尾空白才改
      AND n.trimmed <> '' -- 全是空白的保持原样
      AND NOT EXISTS (
          SELECT 1
          FROM norm AS m
          WHERE m.id <> n.id
            AND m.trimmed = n.trimmed
      ) -- 去掉后与别的账号同名则不动
)
UPDATE people AS p
SET login_name = movable.trimmed
FROM movable
WHERE p.id = movable.id;
