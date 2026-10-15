-- +goose Up
-- 内置频道里把 CGTN纪录 写成了 CGTN记录（线上旧库在 2026-06 已改正，种子数据没有同步）。
-- 央视频、咪咕的「CGTN外语纪录」别名是 CGTN纪录，写错后自动映射匹配不上。
-- 00003 已随 1.0.1 发布，不能再改，在这里修正。
-- channel_mappings、programs 的外键都是 ON UPDATE CASCADE，改 channel_id 会一并更新。
-- 正则同时接受常见的错别字「记录」。
UPDATE channels
SET channel_id   = 'CGTN纪录',
    display_name = 'CGTN纪录',
    regexp       = '^cgtn\s*(纪录|记录|documentary)',
    updated_at   = now()
WHERE channel_id = 'CGTN记录'
  AND NOT EXISTS (SELECT 1 FROM channels WHERE channel_id = 'CGTN纪录');

-- +goose Down
UPDATE channels
SET channel_id   = 'CGTN记录',
    display_name = 'CGTN记录',
    regexp       = '^cgtn\s*(记录|documentary)',
    updated_at   = now()
WHERE channel_id = 'CGTN纪录'
  AND NOT EXISTS (SELECT 1 FROM channels WHERE channel_id = 'CGTN记录');
