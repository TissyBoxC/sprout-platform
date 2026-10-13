#!/usr/bin/env bash
# Guard the worker's shell contract before publishing a release image.
set -euo pipefail

script_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
worker_script="$script_dir/service-upgrade-worker.sh"
compose_file="$script_dir/../docker-compose.yml"

bash -n "$worker_script"

if ! grep -Eq '^require_command\(\)' "$worker_script"; then
  echo "service-upgrade-worker.sh 缺少 require_command 定义。" >&2
  exit 1
fi

if ! grep -Eq '^prepare_worker_runtime_copy\(\)' "$worker_script"; then
  echo "service-upgrade-worker.sh 缺少运行时副本准备逻辑。" >&2
  exit 1
fi

worker_runtime_block="$(
  sed -n '/^prepare_worker_runtime_copy()/,/^}/p' "$worker_script"
)"
if ! printf '%s\n' "$worker_runtime_block" | grep -Fq 'cp "${BASH_SOURCE[0]}" "$runtime_copy"'; then
  echo "service-upgrade-worker.sh 的运行时副本未复制自身。" >&2
  exit 1
fi

worker_main_block="$(
  sed -n '/^main()/,/^}/p' "$worker_script"
)"
if ! printf '%s\n' "$worker_main_block" | grep -Fq 'exec bash "$runtime_copy" "$@"'; then
  echo "service-upgrade-worker.sh 未从运行时副本重新执行。" >&2
  exit 1
fi
if ! printf '%s\n' "$worker_main_block" | grep -Fq 'SPROUT_SERVICE_UPGRADE_WORKER_RUNTIME_COPY="$runtime_copy"'; then
  echo "service-upgrade-worker.sh 未传递运行时副本标记。" >&2
  exit 1
fi
if ! printf '%s\n' "$worker_main_block" | grep -Fq 'SPROUT_UPGRADE_CLOUD_DIR="$cloud_dir"'; then
  echo "service-upgrade-worker.sh 未向运行时副本传递原始 cloud_dir。" >&2
  exit 1
fi

if ! grep -Eq '^release_catalog_is_valid\(\)' "$worker_script"; then
  echo "service-upgrade-worker.sh 缺少版本目录有效性校验。" >&2
  exit 1
fi

if ! grep -Eq '^refresh_release_catalog\(\)' "$worker_script"; then
  echo "service-upgrade-worker.sh 缺少版本目录刷新与退避逻辑。" >&2
  exit 1
fi

resolve_latest_block="$(
  sed -n '/^resolve_latest_release()/,/^}/p' "$worker_script"
)"
if ! printf '%s\n' "$resolve_latest_block" | grep -Eq 'cached_version'; then
  echo "resolve_latest_release 缺少 GitHub 失败时的缓存回退。" >&2
  exit 1
fi

if ! grep -Eq '^platform_repository=' "$worker_script" ||
  ! grep -Eq '^sub2api_repository=' "$worker_script"; then
  echo "service-upgrade-worker.sh 缺少发布仓库配置。" >&2
  exit 1
fi

upgrade_script="$script_dir/upgrade-service.sh"
bash -n "$upgrade_script"

if ! grep -Eq '^assert_platform_checkout_version\(\)' "$upgrade_script"; then
  echo "upgrade-service.sh 缺少部署源码版本门禁。" >&2
  exit 1
fi

if ! grep -Eq '^prepare_upgrade_executor_runtime_copy\(\)' "$upgrade_script"; then
  echo "upgrade-service.sh 缺少运行时副本准备逻辑。" >&2
  exit 1
fi

executor_runtime_block="$(
  sed -n '/^prepare_upgrade_executor_runtime_copy()/,/^}/p' "$upgrade_script"
)"
if ! printf '%s\n' "$executor_runtime_block" | grep -Fq 'cp "${BASH_SOURCE[0]}" "$runtime_copy"'; then
  echo "upgrade-service.sh 的运行时副本未复制自身。" >&2
  exit 1
fi

executor_launch_block="$(
  sed -n '/^if \[ "${BASH_SOURCE\[0\]}" = "$0" \]; then/,/^fi$/p' "$upgrade_script"
)"
if ! printf '%s\n' "$executor_launch_block" | grep -Fq 'exec bash "$runtime_copy" "$@"'; then
  echo "upgrade-service.sh 未从运行时副本重新执行。" >&2
  exit 1
fi
if ! printf '%s\n' "$executor_launch_block" | grep -Fq 'SPROUT_UPGRADE_CLOUD_DIR="$cloud_dir"'; then
  echo "upgrade-service.sh 未向运行时副本传递原始 cloud_dir。" >&2
  exit 1
fi

executor_exec_line="$(grep -n 'exec bash "$runtime_copy" "$@"' "$upgrade_script" | head -n 1 | cut -d: -f1)"
executor_main_line="$(grep -n '^  main "$@"$' "$upgrade_script" | head -n 1 | cut -d: -f1)"
if [ -z "$executor_exec_line" ] || [ -z "$executor_main_line" ] || [ "$executor_exec_line" -ge "$executor_main_line" ]; then
  echo "upgrade-service.sh 必须在执行主流程前切换到运行时副本。" >&2
  exit 1
fi

if ! grep -Eq '^sync_platform_checkout\(\)' "$upgrade_script"; then
  echo "upgrade-service.sh 缺少发布源码同步。" >&2
  exit 1
fi

sync_block="$(
  sed -n '/^sync_platform_checkout()/,/^}/p' "$upgrade_script"
)"
if ! printf '%s\n' "$sync_block" | grep -Fq 'git -C "$repository_root" fetch'; then
  echo "发布源码同步未拉取目标标签。" >&2
  exit 1
fi
if ! printf '%s\n' "$sync_block" | grep -Fq 'git -C "$repository_root" checkout --detach "$release_tag"'; then
  echo "发布源码同步未原子切换到目标标签。" >&2
  exit 1
fi
if ! printf '%s\n' "$sync_block" | grep -Fq 'refs/tags/${release_tag}:refs/tags/${release_tag}'; then
  echo "发布源码同步未使用不可变标签引用。" >&2
  exit 1
fi

version_guard_block="$(
  sed -n '/^assert_platform_checkout_version()/,/^}/p' "$upgrade_script"
)"
if ! printf '%s\n' "$version_guard_block" | grep -Fq 'version_file="$repository_root/VERSION"'; then
  echo "部署源码版本门禁未读取云端 checkout 的 VERSION。" >&2
  exit 1
fi
if ! printf '%s\n' "$version_guard_block" | grep -Fq 'repository_root="$cloud_dir/../.."'; then
  echo "部署源码版本门禁的仓库根目录路径不正确。" >&2
  exit 1
fi
if ! printf '%s\n' "$version_guard_block" | grep -Fq 'if [ "$checkout_version" != "$target_version" ]; then'; then
  echo "部署源码版本门禁未拒绝版本不一致。" >&2
  exit 1
fi
if ! printf '%s\n' "$version_guard_block" | grep -Fq 'refs/tags/${release_tag}'; then
  echo "部署源码版本门禁未校验目标发布标签。" >&2
  exit 1
fi
if ! printf '%s\n' "$version_guard_block" | grep -Fq 'if [ "$checkout_head" != "$release_head" ]; then'; then
  echo "部署源码版本门禁未校验 checkout 与发布标签是否一致。" >&2
  exit 1
fi

guard_call_block="$(
  sed -n '/if \[ "$requested_service" != "sub2api" \]; then/,/fi/p' "$upgrade_script"
)"
if ! printf '%s\n' "$guard_call_block" |
  grep -Fq 'assert_platform_checkout_version "$target_platform_version"'; then
  echo "平台服务升级未调用部署源码版本门禁。" >&2
  exit 1
fi

sync_call_line="$(grep -n 'sync_platform_checkout "$target_platform_version"' "$upgrade_script" | head -n 1 | cut -d: -f1)"
guard_call_line="$(grep -n 'assert_platform_checkout_version "$target_platform_version"' "$upgrade_script" | head -n 1 | cut -d: -f1)"
compose_check_line="$(grep -n '^  assert_compose_config$' "$upgrade_script" | head -n 1 | cut -d: -f1)"
if [ -z "$sync_call_line" ] || [ -z "$guard_call_line" ] || [ -z "$compose_check_line" ]; then
  echo "无法确认发布同步、版本门禁与 Compose 校验的调用顺序。" >&2
  exit 1
fi
if [ "$sync_call_line" -gt "$guard_call_line" ] || [ "$guard_call_line" -gt "$compose_check_line" ]; then
  echo "发布同步必须早于版本门禁，版本门禁必须早于 Compose 校验。" >&2
  exit 1
fi

worker_runtime_copy="$(mktemp "${TMPDIR:-/tmp}/sprout-worker-runtime-test.XXXXXX")"
cp "$worker_script" "$worker_runtime_copy"
trap 'rm -f "$worker_runtime_copy"' EXIT
if ! cmp -s "$worker_runtime_copy" "$worker_script"; then
  echo "service-upgrade-worker.sh 的运行时副本内容不一致。" >&2
  exit 1
fi

executor_runtime_copy="$(mktemp "${TMPDIR:-/tmp}/sprout-executor-runtime-test.XXXXXX")"
cp "$upgrade_script" "$executor_runtime_copy"
trap 'rm -f "$worker_runtime_copy" "$executor_runtime_copy"' EXIT
if ! cmp -s "$executor_runtime_copy" "$upgrade_script"; then
  echo "upgrade-service.sh 的运行时副本内容不一致。" >&2
  exit 1
fi

guard_root="$(mktemp -d)"
trap 'rm -rf "$guard_root"' EXIT
guard_repo="$guard_root/repo"
mkdir -p "$guard_repo/deploy/cloud"
printf '%s\n' "1.2.3" > "$guard_repo/VERSION"
git -C "$guard_repo" init -q
git -C "$guard_repo" config user.name "upgrade guard test"
git -C "$guard_repo" config user.email "upgrade-guard@example.invalid"
git -C "$guard_repo" add VERSION
git -C "$guard_repo" commit -qm "test release"
git -C "$guard_repo" tag v1.2.3

(
  # shellcheck source=deploy/cloud/scripts/upgrade-service.sh
  source "$upgrade_script"
  cloud_dir="$guard_repo/deploy/cloud"
  assert_platform_checkout_version "1.2.3"
)

printf '%s\n' "1.2.4" > "$guard_repo/VERSION"
git -C "$guard_repo" add VERSION
git -C "$guard_repo" commit -qm "newer release"
if (
  source "$upgrade_script"
  cloud_dir="$guard_repo/deploy/cloud"
  assert_platform_checkout_version "1.2.3"
) >/dev/null 2>&1; then
  echo "部署源码版本门禁未拒绝版本不一致。" >&2
  exit 1
fi

git -C "$guard_repo" checkout -q v1.2.3
(
  source "$upgrade_script"
  cloud_dir="$guard_repo/deploy/cloud"
  assert_platform_checkout_version "1.2.3"
)

# sync_platform_checkout must advance HEAD to an immutable release tag while
# leaving runtime-only files untouched.
sync_origin="$guard_root/origin"
sync_repo="$guard_root/sync-repo"
git clone -q "$guard_repo" "$sync_origin"
git -C "$sync_origin" config user.name "upgrade sync origin"
git -C "$sync_origin" config user.email "upgrade-sync@example.invalid"
printf '%s\n' "1.2.4" > "$sync_origin/VERSION"
git -C "$sync_origin" add VERSION
git -C "$sync_origin" commit -qm "sync release"
git -C "$sync_origin" tag v1.2.4

git clone -q "$sync_origin" "$sync_repo"
mkdir -p "$sync_repo/deploy/cloud/migration-data" "$sync_repo/deploy/cloud/mosquitto/certs"
printf '%s\n' "runtime-only" > "$sync_repo/deploy/cloud/migration-data/keep.dump"
printf '%s\n' "runtime-cert" > "$sync_repo/deploy/cloud/mosquitto/certs/keep.crt"
printf '%s\n' "runtime-env" > "$sync_repo/deploy/cloud/.env"

(
  source "$upgrade_script"
  cloud_dir="$sync_repo/deploy/cloud"
  sync_platform_checkout "1.2.4"
)

if [ "$(git -C "$sync_repo" describe --tags --exact-match HEAD 2>/dev/null)" != "v1.2.4" ]; then
  echo "发布源码同步未切换到目标标签。" >&2
  exit 1
fi
if [ "$(cat "$sync_repo/deploy/cloud/migration-data/keep.dump")" != "runtime-only" ] ||
  [ "$(cat "$sync_repo/deploy/cloud/mosquitto/certs/keep.crt")" != "runtime-cert" ] ||
  [ "$(cat "$sync_repo/deploy/cloud/.env")" != "runtime-env" ]; then
  echo "发布源码同步破坏了运行时文件。" >&2
  exit 1
fi

validate_service_block="$(
  sed -n '/^validate_service()/,/^}/p' "$worker_script"
)"
for unsupported_service in \
  postgres \
  redis \
  mqtt \
  download_init \
  download_ftp \
  download_http; do
  if printf '%s\n' "$validate_service_block" |
    grep -Eq "(^|[|])${unsupported_service}([|]|$)"; then
    echo "validate_service 不应接受基础设施服务: $unsupported_service" >&2
    exit 1
  fi
done

if ! grep -Eq 'status=.*status\.json' "$compose_file" ||
  ! grep -Eq 'generated=.*jq .*generated_at' "$compose_file" ||
  ! grep -Eq 'age.*-le 180' "$compose_file"; then
  echo "upgrade_worker 缺少 status.json 新鲜度健康检查。" >&2
  exit 1
fi

if grep -Eq 'python3' "$compose_file"; then
  echo "upgrade_worker 健康检查依赖容器内不存在的 python3。" >&2
  exit 1
fi

# read_running_image_tag must strip an @sha256 digest before extracting the
# tag; otherwise digest-pinned images surface the digest as the version.
read_tag_block="$(
  sed -n '/^read_running_image_tag()/,/^}/p' "$worker_script"
)"
if ! printf '%s\n' "$read_tag_block" | grep -Eq "awk -F@"; then
  echo "read_running_image_tag 未剥离 @sha256 摘要，版本号会被误读。" >&2
  exit 1
fi

resolved_tag="$(
  printf '%s\n' 'nginx:1.27-alpine@sha256:00000000000000000000000000000000' |
    awk -F@ '{ print $1 }' |
    awk -F: 'NF > 1 { print $NF }'
)"
if [ "$resolved_tag" != "1.27-alpine" ]; then
  echo "read_running_image_tag 摘要剥离结果不正确: $resolved_tag" >&2
  exit 1
fi

echo "service-upgrade-worker shell contract passed."
