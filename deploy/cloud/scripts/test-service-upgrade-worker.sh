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
if ! printf '%s\n' "$resolve_latest_block" | grep -Eq 'connect-timeout|max-time'; then
  echo "resolve_latest_release 缺少有界网络超时。" >&2
  exit 1
fi

release_catalog_block="$(
  sed -n '/^write_release_catalog()/,/^}/p' "$worker_script"
)"
if ! printf '%s\n' "$release_catalog_block" | grep -Fq 'release_catalog_fresh=0'; then
  echo "write_release_catalog 未区分刷新成功和保留旧目录。" >&2
  exit 1
fi

status_snapshot_block="$(
  sed -n '/^write_status_snapshot()/,/^}/p' "$worker_script"
)"
if ! printf '%s\n' "$status_snapshot_block" | grep -Fq 'catalog_fresh'; then
  echo "write_status_snapshot 未区分目录新鲜度和单服务请求失败。" >&2
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

if ! grep -Eq '^assert_no_managed_image_override\(\)' "$upgrade_script"; then
  echo "upgrade-service.sh 缺少受管镜像覆盖门禁。" >&2
  exit 1
fi

if ! grep -Eq '^check_managed_image_override\(\)' "$upgrade_script"; then
  echo "upgrade-service.sh 缺少受管镜像覆盖只读检查。" >&2
  exit 1
fi

override_guard_block="$(
  sed -n '/^assert_no_managed_image_override()/,/^}/p' "$upgrade_script"
)"
if ! printf '%s\n' "$override_guard_block" | grep -Fq 'check_managed_image_override'; then
  echo "受管镜像覆盖门禁未复用只读检查。" >&2
  exit 1
fi
if ! printf '%s\n' "$override_guard_block" | grep -Fq 'exit 1'; then
  echo "受管镜像覆盖门禁未拒绝覆盖。" >&2
  exit 1
fi

override_check_block="$(
  sed -n '/^check_managed_image_override()/,/^}/p' "$upgrade_script"
)"
if ! printf '%s\n' "$override_check_block" | grep -Fq 'device_platform|voice_gateway|admin_web|sub2api'; then
  echo "受管镜像覆盖检查未限定受管服务。" >&2
  exit 1
fi

override_call_block="$(
  sed -n '/^  assert_git_clean$/,/^  assert_no_managed_image_override$/p' "$upgrade_script"
)"
if ! printf '%s\n' "$override_call_block" | grep -Fq 'assert_no_managed_image_override'; then
  echo "升级主流程未调用受管镜像覆盖门禁。" >&2
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

override_root="$(mktemp -d)"
trap 'rm -rf "$override_root"' EXIT
mkdir -p "$override_root/deploy/cloud"
cp "$upgrade_script" "$override_root/deploy/cloud/scripts-upgrade-service.sh"
printf '%s\n' 'services:' '  device_platform:' '    image: sprout-device-platform:0.22.0' > "$override_root/deploy/cloud/docker-compose.override.yml"
override_output="$(
  source "$upgrade_script"
  cloud_dir="$override_root/deploy/cloud"
  check_managed_image_override
)"
if [ "$override_output" != "device_platform" ]; then
  echo "受管镜像覆盖只读检查结果不正确: $override_output" >&2
  exit 1
fi
if (
  source "$upgrade_script"
  cloud_dir="$override_root/deploy/cloud"
  assert_no_managed_image_override
) >/dev/null 2>&1; then
  echo "受管镜像覆盖门禁未拒绝 device_platform 本地镜像覆盖。" >&2
  exit 1
fi

printf '%s\n' 'services:' '  postgres:' '    image: local-postgres:16' > "$override_root/deploy/cloud/docker-compose.override.yml"
(
  source "$upgrade_script"
  cloud_dir="$override_root/deploy/cloud"
  assert_no_managed_image_override
)

# assert_git_clean must tolerate runtime artifacts while still rejecting real
# source changes. Git reports paths relative to the repository root, so this
# covers the deploy/cloud prefix used in production.
clean_root="$(mktemp -d)"
trap 'rm -rf "$override_root" "$clean_root"' EXIT
clean_repo="$clean_root/repo"
mkdir -p \
  "$clean_repo/deploy/cloud/scripts" \
  "$clean_repo/deploy/cloud/migration-data" \
  "$clean_repo/deploy/cloud/mosquitto/certs" \
  "$clean_repo/deploy/cloud/.codex-backups"
cp "$upgrade_script" "$clean_repo/deploy/cloud/scripts/upgrade-service.sh"
git -C "$clean_repo" init -q
git -C "$clean_repo" config user.name "upgrade clean test"
git -C "$clean_repo" config user.email "upgrade-clean@example.invalid"
git -C "$clean_repo" add deploy/cloud/scripts/upgrade-service.sh
git -C "$clean_repo" commit -qm "clean baseline"
printf '%s\n' "runtime-data" > "$clean_repo/deploy/cloud/migration-data/keep.dump"
printf '%s\n' "runtime-cert" > "$clean_repo/deploy/cloud/mosquitto/certs/keep.crt"
printf '%s\n' "runtime-backup" > "$clean_repo/deploy/cloud/.codex-backups/keep.yml"
# The production override is an operator-local runtime file, not a source
# change. The generic cleanliness guard has to let it through so the dedicated
# per-service override guard decides which service (if any) is blocked.
printf '%s\n' \
  'services:' \
  '  postgres:' \
  '    image: local-postgres:16' > "$clean_repo/deploy/cloud/docker-compose.override.yml"
(
  source "$upgrade_script"
  cloud_dir="$clean_repo/deploy/cloud"
  assert_git_clean
)
(
  source "$upgrade_script"
  cloud_dir="$clean_repo/deploy/cloud"
  assert_no_managed_image_override
)
# A managed-service override must still pass the generic cleanliness guard and
# be rejected by the dedicated guard, otherwise the checkout would be rejected
# wholesale instead of only the service the override pins.
printf '%s\n' \
  'services:' \
  '  device_platform:' \
  '    image: sprout-device-platform:0.22.0' > "$clean_repo/deploy/cloud/docker-compose.override.yml"
(
  source "$upgrade_script"
  cloud_dir="$clean_repo/deploy/cloud"
  assert_git_clean
)
if (
  source "$upgrade_script"
  cloud_dir="$clean_repo/deploy/cloud"
  assert_no_managed_image_override
) >/dev/null 2>&1; then
  echo "受管镜像覆盖门禁未拒绝 device_platform 本地覆盖。" >&2
  exit 1
fi
# A tracked override that was edited is a real source change and must still be
# rejected by the generic guard.
git -C "$clean_repo" add deploy/cloud/docker-compose.override.yml
git -C "$clean_repo" commit -qm "track override"
printf '%s\n' "extra" >> "$clean_repo/deploy/cloud/docker-compose.override.yml"
if (
  source "$upgrade_script"
  cloud_dir="$clean_repo/deploy/cloud"
  assert_git_clean
) >/dev/null 2>&1; then
  echo "Git 清洁门禁未拒绝已跟踪 override 的未提交修改。" >&2
  exit 1
fi
printf '%s\n' "source-change" > "$clean_repo/deploy/cloud/source-change.txt"
if (
  source "$upgrade_script"
  cloud_dir="$clean_repo/deploy/cloud"
  assert_git_clean
) >/dev/null 2>&1; then
  echo "Git 清洁门禁未拒绝真实源码变更。" >&2
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

check_stack_script="$script_dir/check-stack.sh"
bash -n "$check_stack_script"
if ! grep -Fq 'check-managed-image-override' "$check_stack_script"; then
  echo "check-stack.sh 未检查本地受管镜像覆盖。" >&2
  exit 1
fi
if ! grep -Fq 'status.json fresh' "$check_stack_script"; then
  echo "check-stack.sh 未检查升级 worker 状态新鲜度。" >&2
  exit 1
fi
if ! grep -Fq 'upgrade_worker' "$check_stack_script"; then
  echo "check-stack.sh 未检查升级 worker 容器。" >&2
  exit 1
fi
if ! grep -Fq 'admin API proxy' "$check_stack_script" ||
  ! grep -Fq '401' "$check_stack_script" ||
  ! grep -Fq '/api/v1/admin/service-versions' "$check_stack_script"; then
  echo "check-stack.sh 未验证管理员 API 的同源代理。" >&2
  exit 1
fi

check_stack_block="$(cat "$check_stack_script")"
for permission_probe in \
  'test -r "$state" -a -w "$state" -a -x "$state"' \
  'test -r "$state/queue" -a -w "$state/queue" -a -x "$state/queue"' \
  'test -r "$state/results" -a -w "$state/results" -a -x "$state/results"'; do
  if ! printf '%s\n' "$check_stack_block" | grep -Fq "$permission_probe"; then
    echo "check-stack.sh 缺少升级状态目录权限检查: $permission_probe" >&2
    exit 1
  fi
done

if ! printf '%s\n' "$check_stack_block" | grep -Fq 'device platform can write the upgrade queue'; then
  echo "check-stack.sh 未验证 device_platform 对升级队列的访问。" >&2
  exit 1
fi

compose_check_block="$(cat "$compose_file")"
if ! printf '%s\n' "$compose_check_block" |
  grep -Fq 'user: "${SPROUT_DEVICE_PLATFORM_UID}:${SPROUT_DEVICE_PLATFORM_GID}"'; then
  echo "device_platform 必须使用平台 UID/GID 运行以匹配升级卷权限。" >&2
  exit 1
fi
if ! printf '%s\n' "$compose_check_block" |
  grep -Fq 'group_add:'; then
  echo "服务缺少共享卷组。" >&2
  exit 1
fi
if ! printf '%s\n' "$compose_check_block" |
  grep -Fq '${SPROUT_DOWNLOAD_SFTP_GID}'; then
  echo "device_platform 缺少共享发布文件组。" >&2
  exit 1
fi
if ! printf '%s\n' "$compose_check_block" |
  grep -Fq 'SPROUT_SERVICE_VERSION_STATE_GID'; then
  echo "upgrade_worker 未使用共享升级卷 GID。" >&2
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

# Status snapshots must mark the running version and newest release in the
# selectable release list. Hard-coded false values made the console consider
# every version non-current and non-latest even when status was "current".
release_annotation_block="$(
  sed -n '/^annotate_releases_for_service()/,/^}/p' "$worker_script"
)"
if ! printf '%s\n' "$release_annotation_block" | grep -Fq 'is_current:'; then
  echo "annotate_releases_for_service 未标记当前版本。" >&2
  exit 1
fi
if ! printf '%s\n' "$release_annotation_block" | grep -Fq 'is_latest:'; then
  echo "annotate_releases_for_service 未标记最新版本。" >&2
  exit 1
fi

if ! grep -Fq 'annotate_releases_for_service' <<<"$(
  sed -n '/^write_status_snapshot()/,/^}/p' "$worker_script"
)"; then
  echo "write_status_snapshot 未调用版本标记逻辑。" >&2
  exit 1
fi

annotated_releases="$(
  source "$worker_script"
  annotate_releases_for_service \
    '[{"version":"0.22.0"},{"version":"0.21.0"},{"version":"0.20.0"}]' \
    "0.22.0" \
    "0.22.0"
)"
if ! jq -e '
  length == 3
  and .[0].is_current == true
  and .[0].is_latest == true
  and .[1].is_current == false
  and .[1].is_latest == false
  and .[2].is_current == false
  and .[2].is_latest == false
' <<<"$annotated_releases" >/dev/null; then
  echo "版本标记结果不正确: $annotated_releases" >&2
  exit 1
fi

if ! grep -Eq '^recover_stale_processing_operations\(\)' "$worker_script"; then
  echo "service-upgrade-worker.sh 缺少 processing 残留恢复逻辑。" >&2
  exit 1
fi
if ! printf '%s\n' "$worker_main_block" | grep -Fq 'recover_stale_processing_operations'; then
  echo "worker 主流程未在启动时恢复 processing 残留。" >&2
  exit 1
fi

# ---------------------------------------------------------------------------
# A worker restart that happens mid-operation must not strand the request in
# processing/: the claim was taken with mv -n and the terminal result was never
# written, so nothing else would advance it. Recovery has to publish an explicit
# failed result and clear the stale claim.
# ---------------------------------------------------------------------------
recovery_root="$(mktemp -d)"
mkdir -p "$recovery_root/processing" "$recovery_root/results"
cat > "$recovery_root/processing/op-recover-1.json" <<'EOF'
{"id":"op-recover-1","target_service":"admin_web","target_version":"0.2.0","requested_at":"2026-01-01T00:00:00Z","requested_by":"recovery-test"}
EOF
(
  source "$worker_script"
  root_dir="$recovery_root"
  queue_dir="$root_dir/queue"
  processing_dir="$root_dir/processing"
  results_dir="$root_dir/results"
  release_cache_dir="$root_dir/.release-cache"
  state_uid=0
  state_gid=0
  read_running_image_version() { printf '%s\n' "0.1.0"; }
  recover_stale_processing_operations
)
if [ -n "$(find "$recovery_root/processing" -type f -name '*.json' -print -quit)" ]; then
  echo "processing 残留未被清理。" >&2
  exit 1
fi
if [ ! -f "$recovery_root/results/op-recover-1.json" ]; then
  echo "processing 残留未生成终态结果。" >&2
  exit 1
fi
if ! jq -e '
  .id == "op-recover-1"
  and .status == "failed"
  and .target_service == "admin_web"
  and .target_version == "0.2.0"
  and .current_version == "0.1.0"
  and .requested_by == "recovery-test"
  and (.finished_at | type == "string")
  and (.log_tail | type == "string")
' "$recovery_root/results/op-recover-1.json" >/dev/null; then
  echo "processing 残留恢复结果内容不正确。" >&2
  cat "$recovery_root/results/op-recover-1.json" >&2
  exit 1
fi
rm -rf "$recovery_root"

# ---------------------------------------------------------------------------
# Fault injection: real upgrade-service.sh functions run against a fake docker
# boundary so pull failures, compose-up failures, and health failures can be
# injected. The executor must leave .env and the running images on the previous
# release whenever the upgrade does not complete.
# ---------------------------------------------------------------------------
fault_root="$(mktemp -d)"
fault_cloud="$fault_root/deploy/cloud"
fault_tmp="$fault_root/tmp"
mkdir -p "$fault_cloud/scripts" "$fault_tmp"
printf '%s\n' 'services: {}' > "$fault_cloud/docker-compose.yml"
cat > "$fault_cloud/scripts/validate-cloud-env.sh" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
chmod +x "$fault_cloud/scripts/validate-cloud-env.sh"
export TMPDIR="$fault_tmp"

reset_fault_env() {
  printf '%s\n' \
    'SPROUT_PLATFORM_VERSION=0.1.0' \
    'SPROUT_SUB2API_VERSION=0.1.0' > "$fault_cloud/.env"
}

# Runs upgrade_services in a subshell with the real executor functions and the
# fake docker boundary. Fault knobs, all optional:
#   FAULT_PULL_FAIL                            - every image pull fails
#   FAULT_UP_FAIL                              - every Compose recreate fails
#   FAULT_HEALTH_SERVICE + FAULT_HEALTH_STATUS - one service never becomes ready
run_upgrade_fault_case() {
  requested_service="$1"
  requested_version="$2"
  expected_exit="$3"
  fault_log="$4"
  : > "$fault_log"

  # Prefix assignments before a function call are not guaranteed to reach the
  # nested subshell, so export the knobs explicitly before dropping into it.
  export FAULT_CURRENT_VERSION="${FAULT_CURRENT_VERSION:-0.1.0}"
  export FAULT_PULL_FAIL="${FAULT_PULL_FAIL:-0}"
  export FAULT_UP_FAIL="${FAULT_UP_FAIL:-0}"
  export FAULT_HEALTH_SERVICE="${FAULT_HEALTH_SERVICE:-}"
  export FAULT_HEALTH_STATUS="${FAULT_HEALTH_STATUS:-starting}"
  export FAULT_HEALTH_TIMEOUT="${FAULT_HEALTH_TIMEOUT:-120}"

  # Deliberately not `if ( ... ); then`: wrapping the subshell in an `if`
  # condition suppresses `set -e` and the ERR trap inside it, which would hide
  # exactly the failure propagation this test exists to verify.
  set +e
  (
    set -euo pipefail
    # shellcheck source=deploy/cloud/scripts/upgrade-service.sh
    source "$upgrade_script"
    cloud_dir="$fault_cloud"
    compose_file="$cloud_dir/docker-compose.yml"
    env_file="$cloud_dir/.env"
    compose_project_dir="$cloud_dir"
    health_timeout="$FAULT_HEALTH_TIMEOUT"
    platform_repository="example/platform"
    sub2api_repository="example/sub2api"

    export FAULT_LOG="$fault_log"

    # Stub the network and checkout boundaries; the failure paths under test are
    # entirely inside the version switch, Compose recreate, and rollback logic.
    resolve_release_by_version() { resolved_version="$2"; resolved_release_url=""; return 0; }
    resolve_latest_release() { resolved_version="0.2.0"; resolved_release_url=""; return 0; }
    release_version_is_published() { return 0; }
    sync_platform_checkout() { return 0; }
    assert_platform_checkout_version() { return 0; }
    assert_git_clean() { return 0; }

    docker() {
      docker_argv=("$@")
      docker_platform="${SPROUT_PLATFORM_VERSION:-}"
      docker_sub2api="${SPROUT_SUB2API_VERSION:-}"
      docker_env_file=""
      docker_index=0
      while [ "$docker_index" -lt "${#docker_argv[@]}" ]; do
        if [ "${docker_argv[$docker_index]}" = "--env-file" ]; then
          docker_env_file="${docker_argv[$((docker_index + 1))]}"
          break
        fi
        docker_index=$((docker_index + 1))
      done
      # The recreate loop relies on docker compose reading the already-written
      # .env file instead of prefixing the version variables, so mirror that
      # precedence here.
      if [ -z "$docker_platform" ] && [ -n "$docker_env_file" ] && [ -f "$docker_env_file" ]; then
        docker_platform="$(awk -F= '$1 == "SPROUT_PLATFORM_VERSION" { print $2 }' "$docker_env_file")"
        docker_sub2api="$(awk -F= '$1 == "SPROUT_SUB2API_VERSION" { print $2 }' "$docker_env_file")"
      fi
      {
        printf 'PLATFORM=%s SUB2API=%s ARGS=' \
          "$docker_platform" "$docker_sub2api"
        printf '%s ' "$@"
        printf '\n'
      } >> "$FAULT_LOG"

      if [ "$1" = "compose" ]; then
        shift
        while [ "$#" -gt 0 ]; do
          case "$1" in
            --project-directory|-f|--env-file) shift 2 ;;
            *) break ;;
          esac
        done
        subcommand="$1"
        shift
        case "$subcommand" in
          config)
            return 0
            ;;
          ps)
            ps_service=""
            for ps_arg in "$@"; do
              ps_service="$ps_arg"
            done
            printf '%s\n' "cid-${ps_service}"
            return 0
            ;;
          pull)
            pull_service=""
            for pull_arg in "$@"; do
              pull_service="$pull_arg"
            done
            if [ "$FAULT_PULL_FAIL" = "1" ]; then
              printf '%s\n' "injected pull failure: ${pull_service}" >&2
              return 1
            fi
            return 0
            ;;
          up)
            up_service=""
            for up_arg in "$@"; do
              up_service="$up_arg"
            done
            if [ "$FAULT_UP_FAIL" = "1" ]; then
              printf '%s\n' "injected up failure: ${up_service}" >&2
              return 1
            fi
            return 0
            ;;
        esac
        return 0
      fi

      if [ "$1" = "inspect" ]; then
        inspect_format="$3"
        inspect_container="$4"
        inspect_service="${inspect_container#cid-}"
        case "$inspect_format" in
          *Health*)
            if [ -n "$FAULT_HEALTH_SERVICE" ] && [ "$inspect_service" = "$FAULT_HEALTH_SERVICE" ]; then
              printf '%s\n' "$FAULT_HEALTH_STATUS"
            else
              printf '%s\n' "healthy"
            fi
            return 0
            ;;
          *Config.Image*)
            printf '%s\n' "ghcr.io/example/service:${FAULT_CURRENT_VERSION}"
            return 0
            ;;
        esac
        printf '%s\n' "healthy"
        return 0
      fi
      return 0
    }

    if [ -n "$requested_version" ]; then
      upgrade_services "$requested_service" "$requested_version"
    else
      upgrade_services "$requested_service"
    fi
  )
  actual_exit=$?
  set -e

  if [ "$actual_exit" -ne "$expected_exit" ]; then
    echo "故障用例 ${requested_service} 退出码应为 ${expected_exit}，实际 ${actual_exit}。" >&2
    cat "$fault_log" >&2
    exit 1
  fi
}

assert_fault_env_versions() {
  expected_platform="$1"
  expected_sub2api="$2"
  actual_platform="$(awk -F= '$1 == "SPROUT_PLATFORM_VERSION" { print $2 }' "$fault_cloud/.env")"
  actual_sub2api="$(awk -F= '$1 == "SPROUT_SUB2API_VERSION" { print $2 }' "$fault_cloud/.env")"
  if [ "$actual_platform" != "$expected_platform" ] ||
    [ "$actual_sub2api" != "$expected_sub2api" ]; then
    echo ".env 版本应为 platform=${expected_platform} sub2api=${expected_sub2api}，实际 platform=${actual_platform} sub2api=${actual_sub2api}。" >&2
    exit 1
  fi
}

assert_up_call_with_platform() {
  expected_platform="$1"
  service="$2"
  fault_log="$3"
  # Require the version and the service on the same recorded invocation so a
  # match cannot be stitched together from two different log lines.
  if ! awk -v expected="PLATFORM=${expected_platform} " \
    -v target=" up -d --no-deps ${service} " '
      index($0, expected) == 1 && index($0, target) > 0 { found = 1 }
      END { exit(found ? 0 : 1) }
    ' "$fault_log"; then
    echo "未按预期以 platform=${expected_platform} 重建 ${service}。" >&2
    cat "$fault_log" >&2
    exit 1
  fi
}

reset_fault_env
FAULT_PULL_FAIL=1 FAULT_UP_FAIL=0 \
  FAULT_HEALTH_SERVICE="" FAULT_CURRENT_VERSION="0.1.0" \
  run_upgrade_fault_case admin_web 0.2.0 1 "$fault_root/pre-pull.log"
assert_fault_env_versions 0.1.0 0.1.0
if grep -Eq ' up -d ' "$fault_root/pre-pull.log"; then
  echo "镜像预拉取失败后不应重建任何容器。" >&2
  cat "$fault_root/pre-pull.log" >&2
  exit 1
fi

reset_fault_env
FAULT_PULL_FAIL=0 FAULT_UP_FAIL=1 \
  FAULT_HEALTH_SERVICE="" FAULT_CURRENT_VERSION="0.1.0" \
  run_upgrade_fault_case admin_web 0.2.0 1 "$fault_root/up-fail.log"
assert_fault_env_versions 0.1.0 0.1.0
assert_up_call_with_platform 0.2.0 admin_web "$fault_root/up-fail.log"
assert_up_call_with_platform 0.1.0 admin_web "$fault_root/up-fail.log"

reset_fault_env
FAULT_PULL_FAIL=0 FAULT_UP_FAIL=0 \
  FAULT_HEALTH_SERVICE="admin_web" FAULT_HEALTH_STATUS="exited" FAULT_CURRENT_VERSION="0.1.0" \
  run_upgrade_fault_case admin_web 0.2.0 1 "$fault_root/health-exited.log"
assert_fault_env_versions 0.1.0 0.1.0
assert_up_call_with_platform 0.1.0 admin_web "$fault_root/health-exited.log"

reset_fault_env
FAULT_PULL_FAIL=0 FAULT_UP_FAIL=0 \
  FAULT_HEALTH_SERVICE="admin_web" FAULT_HEALTH_STATUS="starting" FAULT_HEALTH_TIMEOUT=10 \
  FAULT_CURRENT_VERSION="0.1.0" \
  run_upgrade_fault_case admin_web 0.2.0 1 "$fault_root/health-timeout.log"
assert_fault_env_versions 0.1.0 0.1.0
assert_up_call_with_platform 0.1.0 admin_web "$fault_root/health-timeout.log"

# Upgrading every service must follow the fixed dependency order and, when the
# last service fails, roll every already-recreated service back to the previous
# release rather than leaving a half-upgraded stack behind.
reset_fault_env
FAULT_PULL_FAIL=0 FAULT_UP_FAIL=0 \
  FAULT_HEALTH_SERVICE="admin_web" FAULT_HEALTH_STATUS="exited" FAULT_CURRENT_VERSION="0.1.0" \
  run_upgrade_fault_case all "" 1 "$fault_root/all-fail.log"
assert_fault_env_versions 0.1.0 0.1.0
ordered_up_services="$(
  grep -E ' up -d --no-deps ' "$fault_root/all-fail.log" |
    grep -E '^PLATFORM=0\.2\.0' |
    sed -E 's/.*up -d --no-deps ([^ ]+) .*/\1/' |
    paste -sd' ' -
)"
if [ "$ordered_up_services" != "sub2api device_platform voice_gateway admin_web" ]; then
  echo "全量升级顺序不正确: $ordered_up_services" >&2
  cat "$fault_root/all-fail.log" >&2
  exit 1
fi
for service in sub2api device_platform voice_gateway admin_web; do
  assert_up_call_with_platform 0.1.0 "$service" "$fault_root/all-fail.log"
done

rm -rf "$fault_root"

echo "service-upgrade-worker shell contract passed."
