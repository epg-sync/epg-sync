"use client"

import { Fragment, useCallback, useEffect, useRef, useState } from "react"
import { ChevronDown, ChevronRight, RefreshCw, Search } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
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
import type { LogEntry } from "@/types"

const PAGE_SIZE = 100
const LIVE_INTERVAL_MS = 3000
// 实时模式下列表只保留最新的这么多条，避免长时间开着页面占用过多内存
const MAX_ROWS = 2000

// 与后端 zap 的级别值一致
const LEVELS: Record<number, { label: string; className: string }> = {
  [-1]: { label: "DEBUG", className: "bg-gray-100 text-gray-600" },
  0: { label: "INFO", className: "bg-blue-50 text-blue-700" },
  1: { label: "WARN", className: "bg-amber-50 text-amber-700" },
  2: { label: "ERROR", className: "bg-red-50 text-red-700" },
}
const FATAL = { label: "FATAL", className: "bg-red-600 text-white" }

const SOURCES: Record<string, string> = {
  "": "业务",
  http: "HTTP",
  gorm: "数据库",
  logs: "日志",
}

const RANGES: Record<string, number> = {
  "1h": 60 * 60 * 1000,
  "24h": 24 * 60 * 60 * 1000,
  "7d": 7 * 24 * 60 * 60 * 1000,
}

interface Filters {
  level: string
  source: string
  range: string
  keyword: string
}

function buildParams(f: Filters, extra: Record<string, string | number>) {
  const params: Record<string, string | number> = { limit: PAGE_SIZE, ...extra }
  if (f.level !== "all") params.level = f.level
  if (f.source !== "all") params.logger = f.source
  if (f.keyword.trim()) params.q = f.keyword.trim()
  if (f.range !== "all") {
    params.since = new Date(Date.now() - RANGES[f.range]).toISOString()
  }
  return params
}

function formatTime(iso: string) {
  const d = new Date(iso)
  const pad = (n: number, w = 2) => String(n).padStart(w, "0")
  return `${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(
    d.getMinutes()
  )}:${pad(d.getSeconds())}.${pad(d.getMilliseconds(), 3)}`
}

function formatValue(v: unknown) {
  if (typeof v === "string") return v
  return JSON.stringify(v)
}

function fieldSummary(fields: LogEntry["fields"]) {
  if (!fields) return ""
  return Object.entries(fields)
    .map(([k, v]) => `${k}=${formatValue(v)}`)
    .join("  ")
}

function LevelBadge({ level }: { level: number }) {
  const meta = LEVELS[level] ?? FATAL
  return (
    <span
      className={`inline-block w-14 rounded px-1.5 py-0.5 text-center text-[11px] font-semibold ${meta.className}`}
    >
      {meta.label}
    </span>
  )
}

export default function LogsPage() {
  const [logs, setLogs] = useState<LogEntry[]>([])
  const [loadingMore, setLoadingMore] = useState(false)
  const [hasMore, setHasMore] = useState(false)
  const [live, setLive] = useState(false)
  const [expanded, setExpanded] = useState<Set<number>>(new Set())
  const [filters, setFilters] = useState<Filters>({
    level: "all",
    source: "all",
    range: "all",
    keyword: "",
  })
  const [keywordInput, setKeywordInput] = useState("")
  const { toast } = useToast()

  // 实时刷新的定时器读取最新状态用
  const logsRef = useRef<LogEntry[]>([])
  logsRef.current = logs
  const filtersRef = useRef(filters)
  filtersRef.current = filters

  // 关键字输入停顿后再查询
  useEffect(() => {
    const timer = setTimeout(() => {
      setFilters((f) => (f.keyword === keywordInput ? f : { ...f, keyword: keywordInput }))
    }, 400)
    return () => clearTimeout(timer)
  }, [keywordInput])

  const showError = useCallback(
    (error: any) => {
      toast({
        variant: "destructive",
        title: "加载失败",
        description: error.response?.data?.error || "无法加载日志",
      })
    },
    [toast]
  )

  // 筛选条件变化或点击刷新时重新加载第一页；loading 由「已加载的查询」是否等于当前查询推出
  const [reloadCount, setReloadCount] = useState(0)
  const query = JSON.stringify(filters) + "#" + reloadCount
  const [loadedQuery, setLoadedQuery] = useState<string | null>(null)
  const loading = loadedQuery !== query

  useEffect(() => {
    let cancelled = false
    api
      .get("/admin/logs", { params: buildParams(filters, {}) })
      .then((response: any) => {
        if (cancelled) return
        const items: LogEntry[] = response.data || []
        setLogs(items)
        setHasMore(items.length === PAGE_SIZE)
        setExpanded(new Set())
      })
      .catch((error) => {
        if (!cancelled) showError(error)
      })
      .finally(() => {
        if (!cancelled) setLoadedQuery(query)
      })
    return () => {
      cancelled = true
    }
  }, [filters, query, showError])

  const reload = () => setReloadCount((n) => n + 1)

  const loadMore = async () => {
    const oldest = logs[logs.length - 1]
    if (!oldest) return
    setLoadingMore(true)
    try {
      const response = await api.get("/admin/logs", {
        params: buildParams(filters, { before_id: oldest.id }),
      })
      const items: LogEntry[] = response.data || []
      setLogs((prev) => [...prev, ...items])
      setHasMore(items.length === PAGE_SIZE)
    } catch (error) {
      showError(error)
    } finally {
      setLoadingMore(false)
    }
  }

  // 实时模式：定时拉取比当前最新一条更新的日志，插到最前面
  useEffect(() => {
    if (!live) return
    let cancelled = false

    const poll = async () => {
      let newestId = logsRef.current[0]?.id ?? 0
      try {
        // 一次最多 PAGE_SIZE 条，积压较多时连续拉取
        for (;;) {
          const response = await api.get("/admin/logs", {
            params: buildParams(filtersRef.current, { after_id: newestId }),
          })
          const items: LogEntry[] = response.data || []
          if (cancelled || items.length === 0) break
          newestId = items[0].id
          if (logsRef.current.length + items.length > MAX_ROWS) setHasMore(true)
          // 筛选条件刚变化时，第一页可能已经包含这些条目，按 id 去重
          setLogs((prev) => {
            const top = prev[0]?.id ?? 0
            return [...items.filter((i) => i.id > top), ...prev].slice(0, MAX_ROWS)
          })
          if (items.length < PAGE_SIZE) break
        }
      } catch {
        // 网络抖动时下一轮再试，不打扰用户
      }
    }

    const timer = setInterval(poll, LIVE_INTERVAL_MS)
    poll()
    return () => {
      cancelled = true
      clearInterval(timer)
    }
  }, [live, filters])

  const toggleExpanded = (id: number) => {
    setExpanded((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  const setFilter = (key: keyof Filters) => (value: string) =>
    setFilters((f) => ({ ...f, [key]: value }))

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold">系统日志</h1>
        <p className="text-gray-500 mt-1">
          程序运行日志，包括同步任务、HTTP 请求和数据库错误
        </p>
      </div>

      <div className="bg-white rounded-lg shadow-sm border border-gray-200 p-6 space-y-4">
        <div className="relative">
          <Search className="absolute left-4 top-1/2 -translate-y-1/2 h-5 w-5 text-gray-400" />
          <Input
            placeholder="搜索消息、字段（如频道、数据源、错误信息）..."
            value={keywordInput}
            onChange={(e) => setKeywordInput(e.target.value)}
            className="pl-12 h-11"
          />
        </div>

        <div className="flex flex-wrap items-end gap-3">
          <div className="space-y-1.5">
            <label className="text-sm font-medium text-gray-700">级别</label>
            <Select value={filters.level} onValueChange={setFilter("level")}>
              <SelectTrigger className="w-36">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">全部</SelectItem>
                <SelectItem value="info">INFO 及以上</SelectItem>
                <SelectItem value="warn">WARN 及以上</SelectItem>
                <SelectItem value="error">仅 ERROR</SelectItem>
              </SelectContent>
            </Select>
          </div>

          <div className="space-y-1.5">
            <label className="text-sm font-medium text-gray-700">来源</label>
            <Select value={filters.source} onValueChange={setFilter("source")}>
              <SelectTrigger className="w-32">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">全部</SelectItem>
                <SelectItem value="app">业务</SelectItem>
                <SelectItem value="http">HTTP 请求</SelectItem>
                <SelectItem value="gorm">数据库</SelectItem>
              </SelectContent>
            </Select>
          </div>

          <div className="space-y-1.5">
            <label className="text-sm font-medium text-gray-700">时间</label>
            <Select value={filters.range} onValueChange={setFilter("range")}>
              <SelectTrigger className="w-32">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">全部</SelectItem>
                <SelectItem value="1h">最近 1 小时</SelectItem>
                <SelectItem value="24h">最近 24 小时</SelectItem>
                <SelectItem value="7d">最近 7 天</SelectItem>
              </SelectContent>
            </Select>
          </div>

          <div className="ml-auto flex items-center gap-4">
            <label className="flex items-center gap-2 text-sm text-gray-700 cursor-pointer">
              <Switch checked={live} onCheckedChange={setLive} />
              实时刷新
              {live && <span className="h-2 w-2 rounded-full bg-green-500 animate-pulse" />}
            </label>
            <Button variant="outline" onClick={reload} disabled={loading}>
              <RefreshCw className={`mr-2 h-4 w-4 ${loading ? "animate-spin" : ""}`} />
              刷新
            </Button>
          </div>
        </div>
      </div>

      <div className="bg-white rounded-lg shadow-sm border border-gray-200 overflow-hidden">
        <div className="border-b border-gray-200 bg-gray-50 px-6 py-3 text-sm font-semibold text-gray-700">
          日志
          <span className="ml-2 font-normal text-gray-500">
            (已加载 {logs.length} 条，从新到旧)
          </span>
        </div>

        {loading ? (
          <div className="p-12 text-center">
            <div className="inline-block animate-spin rounded-full h-8 w-8 border-b-2 border-blue-600"></div>
            <p className="mt-4 text-gray-500">加载中...</p>
          </div>
        ) : logs.length === 0 ? (
          <div className="p-12 text-center text-gray-500">没有符合条件的日志</div>
        ) : (
          <div className="divide-y divide-gray-100 font-mono text-xs">
            {logs.map((log) => {
              const isOpen = expanded.has(log.id)
              const summary = fieldSummary(log.fields)
              return (
                <Fragment key={log.id}>
                  <button
                    type="button"
                    onClick={() => toggleExpanded(log.id)}
                    className={`flex w-full items-start gap-3 px-4 py-1.5 text-left hover:bg-gray-50 ${
                      log.level >= 2 ? "bg-red-50/40" : ""
                    }`}
                  >
                    {isOpen ? (
                      <ChevronDown className="mt-0.5 h-3.5 w-3.5 shrink-0 text-gray-400" />
                    ) : (
                      <ChevronRight className="mt-0.5 h-3.5 w-3.5 shrink-0 text-gray-400" />
                    )}
                    <span className="shrink-0 text-gray-500">{formatTime(log.time)}</span>
                    <LevelBadge level={log.level} />
                    <span className="w-12 shrink-0 text-gray-400">
                      {SOURCES[log.logger] ?? log.logger}
                    </span>
                    <span className="min-w-0 flex-1 truncate">
                      <span className="text-gray-900">{log.message}</span>
                      {summary && <span className="ml-3 text-gray-500">{summary}</span>}
                    </span>
                  </button>
                  {isOpen && (
                    <div className="space-y-3 bg-gray-50 px-11 py-3 text-gray-700">
                      <div className="whitespace-pre-wrap break-all text-gray-900">
                        {log.message}
                      </div>
                      {log.fields && Object.keys(log.fields).length > 0 && (
                        <table className="w-full">
                          <tbody>
                            {Object.entries(log.fields).map(([k, v]) => (
                              <tr key={k} className="align-top">
                                <td className="w-40 py-0.5 pr-4 text-gray-500">{k}</td>
                                <td className="whitespace-pre-wrap break-all py-0.5">
                                  {typeof v === "string" ? v : JSON.stringify(v, null, 2)}
                                </td>
                              </tr>
                            ))}
                          </tbody>
                        </table>
                      )}
                      <div className="text-gray-500">
                        {new Date(log.time).toLocaleString("zh-CN")}
                        {log.caller && <span className="ml-4">{log.caller}</span>}
                      </div>
                      {log.stack && (
                        <pre className="max-h-80 overflow-auto rounded bg-white p-3 text-[11px] text-gray-600 border border-gray-200">
                          {log.stack}
                        </pre>
                      )}
                    </div>
                  )}
                </Fragment>
              )
            })}
          </div>
        )}

        {!loading && hasMore && (
          <div className="border-t border-gray-200 p-4 text-center">
            <Button variant="outline" onClick={loadMore} disabled={loadingMore}>
              {loadingMore ? "加载中..." : "加载更早的日志"}
            </Button>
          </div>
        )}
      </div>
    </div>
  )
}
