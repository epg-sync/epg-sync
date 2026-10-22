-- +goose Up
-- 自定义数据源的覆盖模式。数据按「频道+天」归属，全量源 > 内置 provider > 增量源：
--   full 全量覆盖：某频道某天有节目，这一天整天以文件为准；多个全量源冲突时先添加的（id 小的）优先，
--        节目 provider_id 为 'custom:<id>'
--   fill 增量补充：只补内置源和全量源当天都没有节目的「频道+天」，增量源之间按 priority（数字小的优先），
--        节目 provider_id 为 'custom-fill:<id>'
-- 已有的数据源原本就覆盖内置数据，迁移为 full，provider_id 前缀不变，已写入的节目可以直接沿用。
ALTER TABLE custom_sources
    ADD COLUMN mode text NOT NULL DEFAULT 'full' CHECK (mode IN ('full', 'fill')),
    -- 同步成功时的补充说明，如因冲突跳过的频道日数
    ADD COLUMN last_message text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE custom_sources
    DROP COLUMN last_message,
    DROP COLUMN mode;
