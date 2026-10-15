#!/usr/bin/env bash
# 一条命令发版：选版本号 -> 打 tag -> push。
# tag 推上去后由 .github/workflows/build-push.yml 跑测试、构建镜像、
# 自动创建 GitHub Release，服务器端由 watchtower 自动拉取新镜像并重建容器。
#
# 用法：
#   scripts/release.sh              # 交互选择：patch / minor / major / 自定义
#   scripts/release.sh patch        # 0.0.12 -> 0.0.13
#   scripts/release.sh minor        # 0.0.12 -> 0.1.0
#   scripts/release.sh major        # 0.0.12 -> 1.0.0
#   scripts/release.sh 0.2.0        # 显式指定
#   scripts/release.sh --dry-run    # 只做校验和版本推算，不打 tag

# 脚本依赖 bash 语法；用 sh 运行时（Linux 上通常是 dash）自动切到 bash
if [ -z "${BASH_VERSION:-}" ]; then
  exec bash "$0" "$@"
fi

set -euo pipefail

RELEASE_BRANCH="main"
REMOTE="origin"

die() {
  printf '\033[31merror:\033[0m %s\n' "$1" >&2
  exit 1
}
info() { printf '\033[36m==>\033[0m %s\n' "$1"; }
warn() { printf '\033[33mwarn:\033[0m %s\n' "$1" >&2; }

dry_run=0
bump=""
for arg in "$@"; do
  case "$arg" in
    --dry-run) dry_run=1 ;;
    -h | --help)
      awk 'NR > 1 && !/^#/ { exit } NR > 1' "$0" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *)
      [ -n "$bump" ] && die "参数过多：$arg"
      bump="$arg"
      ;;
  esac
done

cd "$(git rev-parse --show-toplevel)"

# --- 前置校验 -------------------------------------------------------------
branch="$(git rev-parse --abbrev-ref HEAD)"
[ "$branch" = "$RELEASE_BRANCH" ] || die "当前在 $branch，请切到 $RELEASE_BRANCH 再发版"

[ -z "$(git status --porcelain)" ] || die "工作区不干净，请先提交或 stash"

info "同步远端..."
# 历史遗留的 tag（如 0.0.10、0.0.11）在远端被移到了别的提交，本地还指向旧位置，
# git fetch 会对这些 tag 报 "would clobber existing tag" 并以非 0 退出。这类拒绝是逐个 ref 的，
# 分支和其余 tag 照常更新，也不影响版本推算，所以只警告不中断；其它错误仍然直接退出。
fetch_rc=0
fetch_log="$(git fetch --tags --prune "$REMOTE" "$RELEASE_BRANCH" 2>&1)" || fetch_rc=$?
printf '%s\n' "$fetch_log" | grep -v 'would clobber existing tag' | grep -v '^$' || true

if [ "$fetch_rc" -ne 0 ]; then
  stale_tags="$(printf '%s\n' "$fetch_log" |
    sed -n 's/.*\[rejected\][[:space:]]*\([^[:space:]]*\).*would clobber.*/\1/p')"
  other_rejects="$(printf '%s\n' "$fetch_log" | grep '\[rejected\]' | grep -v 'would clobber' || true)"

  if [ -z "$stale_tags" ] || [ -n "$other_rejects" ]; then
    printf '%s\n' "$fetch_log" >&2
    die "git fetch 失败"
  fi

  warn "以下 tag 本地与远端指向不同的提交，已跳过（不影响发版）："
  printf '%s\n' "$stale_tags" | sed 's/^/       /' >&2
  warn "如需与远端对齐：git fetch --tags --force $REMOTE"
fi

local_head="$(git rev-parse HEAD)"
remote_head="$(git rev-parse "$REMOTE/$RELEASE_BRANCH")"
if [ "$local_head" != "$remote_head" ]; then
  die "本地 $RELEASE_BRANCH 与 $REMOTE/$RELEASE_BRANCH 不同步（本地 ${local_head:0:7} / 远端 ${remote_head:0:7}），请先 pull/push"
fi

# --- 推算版本号 -----------------------------------------------------------
# 只认不带 v 前缀的 X.Y.Z tag，与 workflow 的触发规则一致
latest="$(git tag --list '[0-9]*.[0-9]*.[0-9]*' --sort=-v:refname | head -1)"
[ -n "$latest" ] || die "找不到任何形如 X.Y.Z 的 tag，请手动指定版本号"

IFS=. read -r cur_major cur_minor cur_patch <<<"$latest"
next_patch="$cur_major.$cur_minor.$((cur_patch + 1))"
next_minor="$cur_major.$((cur_minor + 1)).0"
next_major="$((cur_major + 1)).0.0"

# 没有给参数时交互选择，直接回车为 patch
if [ -z "$bump" ]; then
  [ -t 0 ] || die "非交互环境请直接传入 patch / minor / major / X.Y.Z"
  echo
  info "当前版本：$latest"
  echo "  1) patch  $next_patch  （修复、小改动）"
  echo "  2) minor  $next_minor  （新功能）"
  echo "  3) major  $next_major  （不兼容的改动）"
  echo "  4) 自定义"
  read -r -p "选择要发布的版本 [1]: " choice
  case "${choice:-1}" in
    1) bump="patch" ;;
    2) bump="minor" ;;
    3) bump="major" ;;
    4) read -r -p "输入版本号 (X.Y.Z): " bump ;;
    *) die "无效的选择：$choice" ;;
  esac
fi

case "$bump" in
  patch) next="$next_patch" ;;
  minor) next="$next_minor" ;;
  major) next="$next_major" ;;
  [0-9]*.[0-9]*.[0-9]*) next="$bump" ;;
  *) die "无法识别的参数 '$bump'（可用：patch / minor / major / X.Y.Z）" ;;
esac

# workflow 只匹配 [0-9]+.[0-9]+.[0-9]+，不合规的 tag 推上去不会触发构建
[[ "$next" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "版本号 '$next' 不是 X.Y.Z 形式，不会触发构建"

if git rev-parse -q --verify "refs/tags/$next" >/dev/null; then
  die "tag $next 已存在"
fi

# --- 确认 -----------------------------------------------------------------
echo
info "$latest -> $next  (${local_head:0:7})"
echo
git log --no-merges --pretty=format:'  · %s' "$latest..HEAD" || true
echo
echo

if [ "$dry_run" = 1 ]; then
  info "--dry-run：未打 tag"
  exit 0
fi

read -r -p "确认发布 $next? [y/N] " reply
case "$reply" in
  y | Y | yes) ;;
  *)
    info "已取消"
    exit 1
    ;;
esac

# --- 打 tag 并推送 ---------------------------------------------------------
git tag -a "$next" -m "release $next"
git push "$REMOTE" "$next"

slug="$(git config --get "remote.$REMOTE.url" | sed -E 's#^(git@|https://)github\.com[:/]##; s#\.git$##')"
echo
info "已推送 tag $next"
info "构建进度：https://github.com/$slug/actions"
info "镜像推送后，服务器上的 watchtower 会在 5 分钟内自动更新，无需登录服务器"
