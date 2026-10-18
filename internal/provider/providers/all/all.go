// Package all 导入所有内置 provider，触发它们的 init 注册。
// 新增 provider 后在这里加一行导入，all_test.go 会检查是否遗漏。
package all

import (
	_ "github.com/epg-sync/epgsync/internal/provider/providers/bfgd"
	_ "github.com/epg-sync/epgsync/internal/provider/providers/btzx"
	_ "github.com/epg-sync/epgsync/internal/provider/providers/cctv"
	_ "github.com/epg-sync/epgsync/internal/provider/providers/daxiang"
	_ "github.com/epg-sync/epgsync/internal/provider/providers/fengshow"
	_ "github.com/epg-sync/epgsync/internal/provider/providers/hainan"
	_ "github.com/epg-sync/epgsync/internal/provider/providers/hebei"
	_ "github.com/epg-sync/epgsync/internal/provider/providers/homeplus"
	_ "github.com/epg-sync/epgsync/internal/provider/providers/iqilu"
	_ "github.com/epg-sync/epgsync/internal/provider/providers/jsp"
	_ "github.com/epg-sync/epgsync/internal/provider/providers/jstv"
	_ "github.com/epg-sync/epgsync/internal/provider/providers/kknews"
	_ "github.com/epg-sync/epgsync/internal/provider/providers/migu"
	_ "github.com/epg-sync/epgsync/internal/provider/providers/shanxi"
	_ "github.com/epg-sync/epgsync/internal/provider/providers/suzhou"
	_ "github.com/epg-sync/epgsync/internal/provider/providers/sxrtv"
	_ "github.com/epg-sync/epgsync/internal/provider/providers/vl"
	_ "github.com/epg-sync/epgsync/internal/provider/providers/xiamen"
	_ "github.com/epg-sync/epgsync/internal/provider/providers/ysp"
)
