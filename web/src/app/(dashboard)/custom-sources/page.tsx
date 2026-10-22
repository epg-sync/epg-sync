"use client"

import { useState, useEffect, useCallback } from "react"
import { Plus, Edit, Trash2, RefreshCw } from "lucide-react"
import { Button } from "@/components/ui/button"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { useToast } from "@/hooks/use-toast"
import api from "@/lib/api"
import type { CustomSource } from "@/types"

type Mode = CustomSource["mode"]

const DEFAULT_PRIORITY = 100

const emptyForm = {
  name: "",
  url: "",
  mode: "full" as Mode,
  enabled: 1,
  priority: String(DEFAULT_PRIORITY),
}

const modeInfo: Record<Mode, { label: string; badge: string; description: string }> = {
  full: {
    label: "全量覆盖",
    badge: "bg-purple-100 text-purple-800",
    description:
      "文件里某个频道某天有节目，这一天就整天以文件为准，覆盖内置节目。多个全量源争同一天时，先添加的优先。",
  },
  fill: {
    label: "增量补充",
    badge: "bg-sky-100 text-sky-800",
    description:
      "只补内置数据源和全量源当天都没有节目的频道。多个增量源之间按优先级，数字越小越优先。",
  },
}

const statusStyles: Record<string, { label: string; className: string }> = {
  running: { label: "同步中", className: "bg-blue-100 text-blue-800" },
  success: { label: "成功", className: "bg-green-100 text-green-800" },
  failed: { label: "失败", className: "bg-red-100 text-red-800" },
}

export default function CustomSourcesPage() {
  const [sources, setSources] = useState<CustomSource[]>([])
  const [loading, setLoading] = useState(true)
  const [isCreateOpen, setIsCreateOpen] = useState(false)
  const [editingSource, setEditingSource] = useState<CustomSource | null>(null)
  const [formData, setFormData] = useState(emptyForm)
  const { toast } = useToast()

  const errorText = (error: any, fallback: string) =>
    error.response?.data?.error || error.response?.data?.message || fallback

  const fetchSources = useCallback(async (): Promise<CustomSource[]> => {
    const response: any = await api.get("/admin/custom-sources")
    return response.data || []
  }, [])

  // 变更后和轮询时刷新列表。silent 时失败不弹提示（轮询每 2 秒一次，不能一直报错）
  const loadSources = useCallback(
    async (silent = false) => {
      try {
        setSources(await fetchSources())
      } catch (error: any) {
        if (!silent) {
          toast({
            variant: "destructive",
            title: "加载失败",
            description: errorText(error, "无法加载数据源列表"),
          })
        }
      }
    },
    [fetchSources, toast]
  )

  useEffect(() => {
    let active = true
    fetchSources()
      .then((list) => active && setSources(list))
      .catch((error: any) => {
        if (active) {
          toast({
            variant: "destructive",
            title: "加载失败",
            description: errorText(error, "无法加载数据源列表"),
          })
        }
      })
      .finally(() => active && setLoading(false))
    return () => {
      active = false
    }
  }, [fetchSources, toast])

  // 有数据源正在同步时，每 2 秒静默刷新一次，直到同步结束
  const hasRunning = sources.some((s) => s.last_status === "running")
  useEffect(() => {
    if (!hasRunning) return
    const timer = setInterval(() => loadSources(true), 2000)
    return () => clearInterval(timer)
  }, [hasRunning, loadSources])

  const closeDialog = () => {
    setIsCreateOpen(false)
    setEditingSource(null)
    setFormData(emptyForm)
  }

  const openEdit = (source: CustomSource) => {
    setEditingSource(source)
    setFormData({
      name: source.name,
      url: source.url,
      mode: source.mode,
      enabled: source.enabled,
      priority: String(source.priority),
    })
  }

  // 返回提交给后端的请求体；校验不通过时弹出提示并返回 null
  const buildPayload = () => {
    const name = formData.name.trim()
    const url = formData.url.trim()
    const priority = Number(formData.priority)
    if (!name || !url) {
      toast({ variant: "destructive", title: "请填写名称和地址" })
      return null
    }
    if (!/^https?:\/\//i.test(url)) {
      toast({ variant: "destructive", title: "地址必须以 http:// 或 https:// 开头" })
      return null
    }
    if (formData.mode !== "fill") {
      // 优先级只对增量源有意义，全量源不提交（后端保持原值）
      return { name, url, mode: formData.mode, enabled: formData.enabled }
    }
    if (!Number.isInteger(priority) || priority < 0 || priority > 100000) {
      toast({ variant: "destructive", title: "优先级必须是 0 到 100000 的整数" })
      return null
    }
    return { name, url, mode: formData.mode, enabled: formData.enabled, priority }
  }

  const handleCreate = async () => {
    const payload = buildPayload()
    if (!payload) return
    try {
      await api.post("/admin/custom-sources", payload)
      toast({
        title: "创建成功",
        description: payload.enabled ? "数据源已创建，正在后台同步" : "数据源已创建",
      })
      closeDialog()
      loadSources()
    } catch (error: any) {
      toast({
        variant: "destructive",
        title: "创建失败",
        description: errorText(error, "创建数据源失败"),
      })
    }
  }

  const PURGE_NOTE =
    "它写入的节目会全部清除：全量源占用过的频道会在后台补抓内置节目，其余数据源会重新同步。\n注意：内置数据源只能补抓当天的节目，之后几天的要等到当天才会抓取。"
  const DISABLE_CONFIRM = `确定停用吗?\n\n${PURGE_NOTE}`
  const MODE_CHANGE_CONFIRM = `切换覆盖模式会先清除按原模式写入的节目，再按新模式重新同步。确定切换吗?\n\n${PURGE_NOTE}`

  const conflictText = (error: any, fallback: string) =>
    error.response?.status === 409 ? "有数据源正在同步，请稍后再试" : errorText(error, fallback)

  const handleUpdate = async () => {
    if (!editingSource) return
    const payload = buildPayload()
    if (!payload) return
    if (editingSource.enabled === 1) {
      if (payload.enabled !== 1) {
        if (!confirm(DISABLE_CONFIRM)) return
      } else if (payload.mode !== editingSource.mode && !confirm(MODE_CHANGE_CONFIRM)) {
        return
      }
    }
    try {
      await api.put(`/admin/custom-sources/${editingSource.id}`, payload)
      toast({ title: "更新成功", description: "数据源已更新" })
      closeDialog()
      loadSources()
    } catch (error: any) {
      toast({
        variant: "destructive",
        title: "更新失败",
        description: conflictText(error, "更新数据源失败"),
      })
    }
  }

  const handleToggle = async (source: CustomSource, enabled: boolean) => {
    if (!enabled && !confirm(DISABLE_CONFIRM)) return
    try {
      await api.put(`/admin/custom-sources/${source.id}`, {
        name: source.name,
        url: source.url,
        enabled: enabled ? 1 : 0,
      })
      loadSources(true)
    } catch (error: any) {
      toast({
        variant: "destructive",
        title: "操作失败",
        description: conflictText(error, "更新数据源失败"),
      })
    }
  }

  const handleDelete = async (source: CustomSource) => {
    if (
      !confirm(
        `确定要删除数据源「${source.name}」吗?\n\n${PURGE_NOTE}\n自动创建的频道会保留。`
      )
    )
      return

    try {
      await api.delete(`/admin/custom-sources/${source.id}`)
      toast({ title: "删除成功", description: "数据源已被删除" })
      loadSources()
    } catch (error: any) {
      toast({
        variant: "destructive",
        title: "删除失败",
        description: conflictText(error, "删除数据源失败"),
      })
    }
  }

  const handleSync = async (source: CustomSource) => {
    try {
      await api.post(`/admin/custom-sources/${source.id}/sync`)
      toast({
        title: "已开始同步",
        description: "正在后台同步所有已启用的数据源",
      })
      loadSources(true)
    } catch (error: any) {
      toast({
        variant: "destructive",
        title: "同步失败",
        description: conflictText(error, "启动同步失败"),
      })
    }
  }

  const formatTime = (value: string | null) =>
    value ? new Date(value).toLocaleString("zh-CN", { hour12: false }) : "-"

  return (
    <div className="space-y-6">
      <div className="flex justify-between items-center">
        <div>
          <h1 className="text-3xl font-bold">数据源</h1>
          <p className="text-gray-500 mt-1">
            添加自己的 XMLTV 节目单文件（支持 .xml 和 .xml.gz）。全量覆盖的数据源优先于内置数据源，
            增量补充的数据源只补内置数据源没有的节目
          </p>
        </div>
        <Button onClick={() => setIsCreateOpen(true)}>
          <Plus className="h-4 w-4 mr-2" />
          新建数据源
        </Button>
      </div>

      {loading ? (
        <div className="text-center py-12">加载中...</div>
      ) : (
        <div className="bg-white rounded-lg shadow">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>名称</TableHead>
                <TableHead>地址</TableHead>
                <TableHead>覆盖模式</TableHead>
                <TableHead>优先级</TableHead>
                <TableHead>启用</TableHead>
                <TableHead>同步状态</TableHead>
                <TableHead>上次同步</TableHead>
                <TableHead>节目数</TableHead>
                <TableHead className="text-right">操作</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {sources.length === 0 && (
                <TableRow>
                  <TableCell colSpan={9} className="text-center text-gray-500 py-8">
                    还没有数据源，点击右上角「新建数据源」添加
                  </TableCell>
                </TableRow>
              )}
              {sources.map((source) => {
                const status = statusStyles[source.last_status]
                return (
                  <TableRow key={source.id}>
                    <TableCell className="font-medium">{source.name}</TableCell>
                    <TableCell
                      className="font-mono text-sm max-w-xs truncate"
                      title={source.url}
                    >
                      {source.url}
                    </TableCell>
                    <TableCell>
                      <span
                        className={`px-2 py-1 rounded text-xs whitespace-nowrap ${modeInfo[source.mode].badge}`}
                        title={modeInfo[source.mode].description}
                      >
                        {modeInfo[source.mode].label}
                      </span>
                    </TableCell>
                    <TableCell>
                      {source.mode === "fill" ? (
                        source.priority
                      ) : (
                        <span className="text-gray-400" title="全量覆盖按添加顺序，先添加的优先">
                          -
                        </span>
                      )}
                    </TableCell>
                    <TableCell>
                      <Switch
                        checked={source.enabled === 1}
                        onCheckedChange={(checked) => handleToggle(source, checked)}
                      />
                    </TableCell>
                    <TableCell>
                      {status ? (
                        <span className={`px-2 py-1 rounded text-xs ${status.className}`}>
                          {status.label}
                        </span>
                      ) : (
                        <span className="text-gray-400 text-sm">未同步</span>
                      )}
                      {source.last_status === "failed" && source.last_error && (
                        <div
                          className="text-xs text-red-600 mt-1 max-w-xs truncate"
                          title={source.last_error}
                        >
                          {source.last_error}
                        </div>
                      )}
                      {source.last_status === "success" && source.last_message && (
                        <div
                          className="text-xs text-gray-500 mt-1 max-w-xs truncate"
                          title={source.last_message}
                        >
                          {source.last_message}
                        </div>
                      )}
                    </TableCell>
                    <TableCell className="text-sm">{formatTime(source.last_sync_at)}</TableCell>
                    <TableCell>{source.program_count}</TableCell>
                    <TableCell className="text-right whitespace-nowrap">
                      <Button
                        variant="ghost"
                        size="sm"
                        title={
                          source.enabled === 1
                            ? "立即同步（同步所有已启用的数据源）"
                            : "已停用，启用后才能同步"
                        }
                        disabled={source.enabled !== 1 || source.last_status === "running"}
                        onClick={() => handleSync(source)}
                      >
                        <RefreshCw
                          className={`h-4 w-4 ${
                            source.last_status === "running" ? "animate-spin" : ""
                          }`}
                        />
                      </Button>
                      <Button
                        variant="ghost"
                        size="sm"
                        title="编辑"
                        onClick={() => openEdit(source)}
                      >
                        <Edit className="h-4 w-4" />
                      </Button>
                      <Button
                        variant="ghost"
                        size="sm"
                        title="删除"
                        onClick={() => handleDelete(source)}
                      >
                        <Trash2 className="h-4 w-4 text-red-500" />
                      </Button>
                    </TableCell>
                  </TableRow>
                )
              })}
            </TableBody>
          </Table>
        </div>
      )}

      <Dialog
        open={isCreateOpen || !!editingSource}
        onOpenChange={(open) => {
          if (!open) closeDialog()
        }}
      >
        <DialogContent className="max-w-md">
          <DialogHeader>
            <DialogTitle>{editingSource ? "编辑数据源" : "新建数据源"}</DialogTitle>
            <DialogDescription>
              文件里的所有频道都会导入，认不出的频道会自动新建
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="name">名称 *</Label>
              <Input
                id="name"
                value={formData.name}
                onChange={(e) => setFormData({ ...formData, name: e.target.value })}
                placeholder="例如: 广东电信 IPTV"
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="url">文件地址 *</Label>
              <Input
                id="url"
                value={formData.url}
                onChange={(e) => setFormData({ ...formData, url: e.target.value })}
                placeholder="http://192.168.1.2/epg.xml.gz"
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="mode">覆盖模式</Label>
              <Select
                value={formData.mode}
                onValueChange={(value) => setFormData({ ...formData, mode: value as Mode })}
              >
                <SelectTrigger id="mode" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="full">{modeInfo.full.label}</SelectItem>
                  <SelectItem value="fill">{modeInfo.fill.label}</SelectItem>
                </SelectContent>
              </Select>
              <p className="text-xs text-gray-500">{modeInfo[formData.mode].description}</p>
            </div>
            {formData.mode === "fill" && (
              <div className="space-y-2">
                <Label htmlFor="priority">优先级</Label>
                <Input
                  id="priority"
                  type="number"
                  min={0}
                  max={100000}
                  value={formData.priority}
                  onChange={(e) => setFormData({ ...formData, priority: e.target.value })}
                />
                <p className="text-xs text-gray-500">
                  数字越小优先级越高；多个增量源能补同一频道的同一天时，以优先级高的为准
                </p>
              </div>
            )}
            <div className="flex items-center gap-3">
              <Label htmlFor="enabled">启用</Label>
              <Switch
                id="enabled"
                checked={formData.enabled === 1}
                onCheckedChange={(checked) =>
                  setFormData({ ...formData, enabled: checked ? 1 : 0 })
                }
              />
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={closeDialog}>
              取消
            </Button>
            <Button onClick={editingSource ? handleUpdate : handleCreate}>
              {editingSource ? "更新" : "创建"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
