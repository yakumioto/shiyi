#!/usr/bin/env bash
# check-readme.sh —— README / Dockerfile / 配置示例 之间的一致性交叉检查。
#
# 为什么需要它：README、Dockerfile、config.example.toml 分属三份交付物，
# 里面的路径、环境变量名、CLI 子命令、错误码很容易互相漂移（例如 README
# 写 `SHIYI_DATA_DIR`、Dockerfile 写 `SHIYI_DATA_PATH`）。本脚本把这些
# 交叉点全部自动比对，漂移时立刻失败。
#
# 用法（脚本自行定位仓库根，可在任意目录执行）：
#     bash docs/check-readme.sh
#
# 退出码：0 = 全部通过；1 = 存在失败项。
#
# 维护约定：新增或修改 README / Dockerfile / config.example.toml 后，
# 重新跑一次本脚本。它不依赖网络，也不依赖 Go 工具链。

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$ROOT" || exit 1

README="README.md"
CONFIG_EXAMPLE="config.example.toml"
CONFIG_DOCKER="docs/config.docker.toml"
DOCKERFILE="Dockerfile"
DOCKERIGNORE=".dockerignore"

CHECKS=0
FAILURES=0

pass() { CHECKS=$((CHECKS + 1)); printf '  [ ok ] %s\n' "$1"; }
fail() { CHECKS=$((CHECKS + 1)); FAILURES=$((FAILURES + 1)); printf '  [FAIL] %s\n' "$1"; }

assert_file() {
	if [ -f "$1" ]; then pass "存在文件 $1"; else fail "缺少文件 $1"; fi
}

assert_contains() { # assert_contains <file> <fixed-string> <desc>
	if [ -f "$1" ] && grep -qF -- "$2" "$1"; then
		pass "$3"
	else
		fail "$3 —— $1 中找不到 “$2”"
	fi
}

assert_absent() { # assert_absent <file> <fixed-string> <desc>
	if [ -f "$1" ] && grep -qF -- "$2" "$1"; then
		fail "$3 —— $1 中不应出现 “$2”"
	else
		pass "$3"
	fi
}

assert_matches() { # assert_matches <file> <ere> <desc>
	if [ -f "$1" ] && grep -qE -- "$2" "$1"; then
		pass "$3"
	else
		fail "$3 —— $1 不匹配 /$2/"
	fi
}

assert_eq() { # assert_eq <actual> <expected> <desc>
	if [ "$1" = "$2" ]; then pass "$3"; else fail "$3 —— 期望 “$2”，实际 “$1”"; fi
}

# assert_no_claim：确认文档没有把「被放弃的能力」写成正面说法。
# 先把否定式（不支持/无/没有/未）抹成同一个标记，再找正面短语，
# 否则「不支持附件」会因为包含「支持附件」而误报。
assert_no_claim() { # assert_no_claim <file> <phrase> <desc>
	local stripped
	stripped=$(sed -e 's/不支持/不X/g' -e 's/没有/不X/g' -e 's/不会/不X/g' \
		-e 's/未支持/不X/g' -e 's/不提供/不X/g' -e 's/无/不X/g' -e 's/未/不X/g' "$1")
	if printf '%s\n' "$stripped" | grep -qF -- "$2"; then
		fail "$3 —— $1 中出现了正面说法 “$2”"
	else
		pass "$3"
	fi
}

section() { printf '\n== %s ==\n' "$1"; }

# ---------------------------------------------------------------- 0. 文件齐备

section "0. 交付文件齐备"
for f in "$DOCKERFILE" "$DOCKERIGNORE" "$README" "$CONFIG_EXAMPLE" Makefile \
	internal/config/config.go internal/api/api.go "$CONFIG_DOCKER" \
	docs/check-readme.sh; do
	assert_file "$f"
done

# ------------------------------------------------- 1. 配置键与 config.go 对齐

section "1. 配置键与 internal/config/config.go 的 toml tag 双向对齐"

# 旧键只用于迁移拒绝，不能强制写回可运行示例。
CONFIG_TAGS=$(grep -h -oE 'toml:"[a-z_]+"' internal/config/config.go internal/config/auth_contract.go \
	| sed 's/toml:"//; s/"$//' | grep -vx 'auth_token_env' | sort -u)
for f in "$CONFIG_EXAMPLE" "$CONFIG_DOCKER"; do
	assert_absent "$f" 'auth_token_env' "配置示例不携带旧 Bearer 键"
	assert_contains "$f" 'password_hash = ""' "示例不嵌入可用密码哈希"
	assert_contains "$f" 'public_origin = ""' "示例要求部署者显式选择来源"
	assert_contains "$f" 'dev_anonymous = false' "示例默认关闭开发匿名"
done

# 标量键：形如 key = value
toml_keys() { # toml_keys <file>
	grep -E '^[[:space:]]*[a-z_]+[[:space:]]*=' "$1" \
		| sed -E 's/^[[:space:]]*([a-z_]+)[[:space:]]*=.*/\1/' | sort -u
}

# 表段名：形如 [name]
toml_sections() { # toml_sections <file>
	grep -oE '^\[[a-z_]+\]' "$1" | tr -d '[]' | sort -u
}

EXAMPLE_KEYS=$(toml_keys "$CONFIG_EXAMPLE")
EXAMPLE_SECTIONS=$(toml_sections "$CONFIG_EXAMPLE")
DOCKERCFG_KEYS=$(toml_keys "$CONFIG_DOCKER")
DOCKERCFG_SECTIONS=$(toml_sections "$CONFIG_DOCKER")

# config.go 里声明的每个 tag，要么是示例里的标量键，要么是示例里的表段。
for t in $CONFIG_TAGS; do
	if printf '%s\n' "$EXAMPLE_KEYS" | grep -qx -- "$t" \
		|| printf '%s\n' "$EXAMPLE_SECTIONS" | grep -qx -- "$t"; then
		pass "$CONFIG_EXAMPLE 覆盖键 $t"
	else
		fail "$CONFIG_EXAMPLE 缺少 config.go 声明的键 $t"
	fi
done
for k in $EXAMPLE_KEYS; do
	if printf '%s\n' "$CONFIG_TAGS" | grep -qx -- "$k"; then
		pass "$CONFIG_EXAMPLE 无未知标量键 $k"
	else
		fail "$CONFIG_EXAMPLE 出现 config.go 未声明的标量键 $k"
	fi
done
for k in $EXAMPLE_SECTIONS; do
	if printf '%s\n' "$CONFIG_TAGS" | grep -qx -- "$k"; then
		pass "$CONFIG_EXAMPLE 无未知表段 [$k]"
	else
		fail "$CONFIG_EXAMPLE 出现 config.go 未声明的表段 [$k]"
	fi
done
for k in $DOCKERCFG_KEYS $DOCKERCFG_SECTIONS; do
	if printf '%s\n' "$CONFIG_TAGS" | grep -qx -- "$k"; then
		pass "$CONFIG_DOCKER 无未知键 $k"
	else
		fail "$CONFIG_DOCKER 出现 config.go 未声明的键 $k"
	fi
done

for s in classifier answer budget auth; do
	if grep -qE "toml:\"$s\"" internal/config/config.go && grep -qE "^\[$s\]" "$CONFIG_EXAMPLE"; then
		pass "$CONFIG_EXAMPLE 存在 [$s] section"
	else
		fail "$CONFIG_EXAMPLE 缺少 [$s] section（或 config.go 未声明）"
	fi
done

# 密钥只能以环境变量名出现，配置文件里不得出现真实密钥形态。
if grep -qE '(api_key|token)[[:space:]]*=[[:space:]]*"[^"]*sk-|Bearer [A-Za-z0-9]{16,}' \
	"$CONFIG_EXAMPLE" "$CONFIG_DOCKER"; then
	fail "配置示例中疑似出现了明文密钥"
else
	pass "配置示例中只有密钥的环境变量名，没有明文密钥"
fi

# ------------------------------------------------------- 2. Dockerfile 契约

section "2. Dockerfile 契约"

assert_matches "$DOCKERFILE" '^FROM .* AS builder' "多阶段构建：存在 builder 阶段"
assert_matches "$DOCKERFILE" '^FROM .* AS runtime' "多阶段构建：存在 runtime 阶段"
assert_matches "$DOCKERFILE" 'go build .*-trimpath.*-ldflags="-s -w".*-o /out/shiyi ./cmd/shiyi' \
	"builder 阶段用 -trimpath -ldflags=\"-s -w\" 构建 ./cmd/shiyi"
assert_matches "$DOCKERFILE" 'GOFLAGS=-mod=vendor' "构建阶段使用 vendor 模式（不联网）"
assert_matches "$DOCKERFILE" 'GOPROXY=off' "构建阶段禁用了模块下载（不联网）"
assert_matches "$DOCKERFILE" 'apk add --no-cache .*ripgrep|apt-get install .*ripgrep' "runtime 阶段安装了 ripgrep"
assert_matches "$DOCKERFILE" '^USER shiyi' "以非 root 用户 shiyi 运行"
assert_matches "$DOCKERFILE" '^EXPOSE 8787' "声明 EXPOSE 8787"
assert_matches "$DOCKERFILE" '^VOLUME /data /auth' "声明独立记忆及认证卷"
assert_matches "$DOCKERFILE" 'chmod 0700 /data /auth' "两个数据目录权限为0700"
assert_matches "$DOCKERFILE" 'chown .*shiyi.*/auth' "认证目录归非root身份所有"
assert_absent "$DOCKERFILE" 'ENV SHIYI_AUTH_PASSWORD_HASH=' "镜像不烘焙密码哈希"
assert_contains "$CONFIG_DOCKER" 'state_dir = "/auth"' "容器认证目录独立于记忆"
assert_matches "$DOCKERFILE" '^ENTRYPOINT \["/usr/local/bin/shiyi"\]' "ENTRYPOINT 指向 /usr/local/bin/shiyi"
assert_matches "$DOCKERFILE" '^CMD \["serve"\]' "CMD 为 serve"
assert_matches "$DOCKERFILE" '^HEALTHCHECK ' "定义了 HEALTHCHECK"
assert_matches "$DOCKERFILE" 'wget .*healthz|curl .*healthz' "HEALTHCHECK 请求 /healthz"
assert_matches "$DOCKERFILE" 'chown .*shiyi.*/data' "/data 归 shiyi 用户所有"

# Dockerfile 里的 SHIYI_CONFIG 必须真的指向镜像内被 COPY 进去的那个文件。
if grep -qE 'COPY .*config\.docker\.toml /etc/shiyi/config\.toml' "$DOCKERFILE"; then
	pass "镜像内的 /etc/shiyi/config.toml 由 $CONFIG_DOCKER 提供"
else
	fail "$DOCKERFILE 未把 $CONFIG_DOCKER 复制到 /etc/shiyi/config.toml"
fi

# .dockerignore 的匹配语义（实测确认）：
#   * 模式相对**构建上下文根**锚定 —— 裸的 `shiyi` 只排除根目录下那个二进制，
#     不会排除 `cmd/shiyi/`；而 `cmd/shiyi` 会排除它。
#   * `*` 不跨 `/`，`**` 跨 `/`，`**/` 可以匹配零层目录。
#   * 命中一个目录就等于排除了它整棵子树。
# 下面按这个语义逐条检查，避免误报也避免漏报。
dockerignore_matches() { # dockerignore_matches <pattern> <path>
	local pat="$1" path="$2" regex
	# 整行注释与行尾注释都要去掉。
	case "$pat" in
	'#'*) return 1 ;;
	esac
	pat="${pat%%#*}"
	pat="$(printf '%s' "$pat" | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')"
	[ -z "$pat" ] && return 1
	case "$pat" in
	'!'*) return 1 ;;   # 取反规则单独出现时不会排除任何东西
	esac
	pat="${pat%/}"
	# 把 glob 模式翻译成正则。占位符用 @@D@@，避免转义层把反斜杠吃掉。
	# 注意顺序：先把 glob 里本来就有的正则元字符转义掉，
	# 再做占位符替换。反过来的话会把 `.*` 里的点也转义掉。
	regex="$(printf '%s' "$pat" \
		| sed -e 's|[.^$+(){}|]|\\&|g' \
		| sed -e 's|\*\*/|@@D@@/|g' -e 's|\*\*|@@D@@|g' \
		| sed -e 's|\*|[^/]*|g' -e 's|@@D@@|.*|g' \
		| sed -e 's|\?|[^/]|g')"
	# 路径本身，或它的任一祖先目录被命中，都算被排除。
	local probe="$path" ancestor
	while :; do
		if printf '%s' "$probe" | grep -qE "^(${regex})(/.*)?$"; then
			return 0
		fi
		ancestor="${probe%/*}"
		[ "$ancestor" = "$probe" ] && break
		probe="$ancestor"
	done
	return 1
}

# 危险情形：.dockerignore 若把 cmd/shiyi 或 vendor 排除掉，构建会直接失败。
# 命中目录等于排除整棵子树；`dir/**` 这类模式只排除目录里的内容，
# 所以既要看目录本身，也要看它下面的一个代表性子路径。
dockerignore_excludes() { # dockerignore_excludes <path> -> 打印命中的模式或空
	local path="$1" line
	while IFS= read -r line; do
		if dockerignore_matches "$line" "$path"; then
			printf '%s' "$line"
			return 0
		fi
		if dockerignore_matches "$line" "$path/PLACEHOLDER"; then
			printf '%s' "$line"
			return 0
		fi
	done < "$DOCKERIGNORE"
	return 1
}

for probe in cmd/shiyi vendor; do
	hit="$(dockerignore_excludes "$probe")"
	if [ -n "$hit" ]; then
		fail ".dockerignore 的 “$hit” 会把 $probe 排除掉（构建会失败）"
	else
		pass ".dockerignore 没有排除 $probe"
	fi
done
for d in '.git' '.scratch' '.worktrees' 'testdata'; do
	if grep -qE "^[[:space:]]*${d}(/|\$)" "$DOCKERIGNORE"; then
		pass ".dockerignore 排除了 $d"
	else
		fail ".dockerignore 未排除 $d"
	fi
done

# --------------------------------------------------- 3. 环境变量名交叉比对

section "3. 环境变量名交叉比对（README ↔ Dockerfile ↔ 配置示例）"

# 解析 Dockerfile 的 ENV（支持一行多个、也支持反斜杠续行）。
# 输出格式：NAME<TAB>VALUE
docker_env() { # docker_env <file>
	awk '
		/^[[:space:]]*#/ { next }
		/^[[:space:]]*$/ { next }
		/^[[:space:]]*ENV[[:space:]]/ {
			line = $0
			while (line ~ /\\$/) {
				sub(/\\$/, "", line)
				if ((getline nxt) <= 0) break
				line = line " " nxt
			}
			sub(/^[[:space:]]*ENV[[:space:]]+/, "", line)
			n = split(line, parts, /[[:space:]]+/)
			for (i = 1; i <= n; i++) {
				if (parts[i] ~ /^[A-Za-z_][A-Za-z0-9_]*=/) {
					eq = index(parts[i], "=")
					print substr(parts[i], 1, eq - 1) "\t" substr(parts[i], eq + 1)
				}
			}
		}
	' "$1"
}

DOCKER_ENV=$(docker_env "$DOCKERFILE")
# 只有 SHIYI_* 是运行时用户要设的变量，必须在 README 里有说明。
DOCKER_RUNTIME_ENV=$(printf '%s\n' "$DOCKER_ENV" | cut -f1 | grep '^SHIYI_' | sort -u)

CONFIG_ENV_NAMES=$(cat "$CONFIG_EXAMPLE" "$CONFIG_DOCKER" \
	| grep -oE '(api_key_env|auth_token_env)[[:space:]]*=[[:space:]]*"[A-Za-z_][A-Za-z0-9_]*"' \
	| sed -E 's/.*"([A-Za-z_][A-Za-z0-9_]*)"/\1/' | sort -u)

# 一般覆盖项与冻结认证契约中的固定名称；旧变量必须作为迁移拒绝文档化。
KNOWN_ENV_OVERRIDES="SHIYI_CONFIG SHIYI_LISTEN SHIYI_DATA_DIR SHIYI_SHARD"
AUTH_ENV_NAMES=$(grep -oE '"SHIYI_[A-Z_]+"' internal/config/auth_contract.go | tr -d '"' | sort -u)

README_ENV_NAMES=$(grep -oE '\b(SHIYI|DEEPSEEK)_[A-Z0-9_]+\b' "$README" | sort -u)

# 用换行连接，避免多个变量名被当成一个词。
ALLOWED_ENV=$(printf '%s\n' "$DOCKER_RUNTIME_ENV" "$CONFIG_ENV_NAMES" "$KNOWN_ENV_OVERRIDES" "$AUTH_ENV_NAMES" \
	| tr ' ' '\n' | grep -v '^$' | sort -u)

for e in $README_ENV_NAMES; do
	if printf '%s\n' "$ALLOWED_ENV" | grep -qx -- "$e"; then
		pass "README 中的 $e 有来源（Dockerfile ENV / 配置里的 *_env / design.md 覆盖项）"
	else
		fail "README 中的 $e 在 Dockerfile / 配置示例里都找不到"
	fi
done

for e in $ALLOWED_ENV; do
	if printf '%s\n' "$README_ENV_NAMES" | grep -qx -- "$e"; then
		pass "$e 在 README 中有说明"
	else
		fail "$e 在 README 中没有出现"
	fi
done

# 容器内配置必须指向一个独立的环境变量名，而不是示例配置里的 DEEPSEEK_API_KEY。
if grep -qE 'api_key_env[[:space:]]*=[[:space:]]*"SHIYI_API_KEY"' "$CONFIG_DOCKER"; then
	pass "$CONFIG_DOCKER 的 api_key_env 指向 SHIYI_API_KEY"
else
	fail "$CONFIG_DOCKER 的 api_key_env 未指向 SHIYI_API_KEY"
fi

# --------------------------------------------------- 4. 路径交叉比对

section "4. 路径交叉比对（/data、配置文件路径、端口）"

env_value() { # env_value <name>
	printf '%s\n' "$DOCKER_ENV" | awk -F'\t' -v k="$1" '$1 == k { print $2 }'
}

assert_eq "$(env_value SHIYI_DATA_DIR)" "/data" "Dockerfile 的 SHIYI_DATA_DIR 为 /data"
assert_eq "$(env_value SHIYI_AUTH_STATE_DIR)" "/auth" "认证状态路径与记忆分离"
assert_contains "$README" '/auth' "README说明独立认证卷"
assert_eq "$(env_value SHIYI_CONFIG)" "/etc/shiyi/config.toml" "Dockerfile 的 SHIYI_CONFIG 为 /etc/shiyi/config.toml"
assert_eq "$(env_value SHIYI_LISTEN)" "0.0.0.0:8787" "Dockerfile 的 SHIYI_LISTEN 为 0.0.0.0:8787"

assert_contains "$README" '/data' "README 说明了容器数据目录 /data"
assert_contains "$README" '/etc/shiyi/config.toml' "README 说明了容器内配置路径 /etc/shiyi/config.toml"
assert_contains "$README" '8787' "README 说明了服务端口 8787"

assert_contains "$CONFIG_DOCKER" 'data_dir = "/data"' "容器配置的 data_dir 为 /data"
assert_contains "$CONFIG_DOCKER" 'listen = "0.0.0.0:8787"' "容器配置的 listen 为 0.0.0.0:8787"

# ------------------------------------------------------ 5. CLI 子命令一致性

section "5. CLI 子命令一致性"

# 实际服务管理入口；不恢复已移除的记忆客户端。
KNOWN_SUBCOMMANDS=$(printf 'serve\nhash-password\n')

FOUND_SUBCOMMANDS=$(grep -oE '\bshiyi[[:space:]]+[a-z][a-z-]*' "$README" \
	| sed -E 's/^shiyi[[:space:]]+//' | sort -u)

for s in $FOUND_SUBCOMMANDS; do
	if printf '%s\n' "$KNOWN_SUBCOMMANDS" | grep -qx -- "$s"; then
		pass "README 中的 shiyi 子命令 “$s” 是已知子命令"
	else
		fail "README 中的 shiyi 子命令 “$s” 不在实际管理命令集合内"
	fi
done

assert_contains "$README" 'shiyi serve' "README 记录了 shiyi serve"
assert_contains "$README" 'shiyi hash-password' "README记录安全哈希管理入口"
assert_contains "$README" '双确认' "README说明交互双确认"
assert_contains "$README" 'TTY' "README说明终端输入约束"
assert_contains "$README" '旧调用明确失败' "README说明CLI迁移"
assert_absent "$README" '/quit' "README不再宣传REPL"
assert_absent cmd/shiyi/options.go '--server' "服务用法不再宣传客户端地址"
assert_contains cmd/shiyi/options.go 'hash-password' "管理命令确实存在"

# README 不得出现其它包里不存在的 CLI 动词（防止文档漂移）。
for bogus in 'shiyi add' 'shiyi query' 'shiyi record' 'shiyi delete' 'shiyi list' 'shiyi config'; do
	assert_absent "$README" "$bogus" "README 未出现未实现的子命令 “$bogus”"
done

# ------------------------------------------------------------ 6. Makefile

section "6. Makefile 目标与门禁命令"

for t in check build test vet test-frozen; do
	if grep -qE "^${t}:" Makefile; then
		pass "Makefile 有 ${t} 目标"
	else
		fail "Makefile 缺少 ${t} 目标"
	fi
	assert_contains "$README" "make ${t}" "README 记录了 make ${t}"
done

assert_contains "$README" 'go test ./... -race' "README 写出了测试门禁 go test ./... -race"
assert_contains "$README" 'go vet ./...' "README 写出了静态检查门禁 go vet ./..."
assert_contains "$README" 'ripgrep' "README 提到了系统依赖 ripgrep"

# ------------------------------------------------------ 7. API 契约一致性

section "7. API 契约一致性（README ↔ internal/api）"

API_CODES=$(grep -h -oE 'Code[A-Za-z]+[[:space:]]*=[[:space:]]*"[a-z_]+"' internal/api/*.go \
	| sed -E 's/.*"([a-z_]+)"/\1/' | sort -u)

README_CODES=$(grep -oE '`[a-z_]+`|"[a-z_]+"' "$README" | tr -d '`"' | sort -u)

for c in $API_CODES; do
	if printf '%s\n' "$README_CODES" | grep -qx -- "$c"; then
		pass "README 记录了错误码 $c"
	else
		fail "README 未记录错误码 $c"
	fi
done

for field in kind time version answer citations status error code message content; do
	assert_contains "$README" "\"$field\"" "README 的示例响应包含字段 \"$field\""
done

assert_contains "$README" '/messages' "README 记录了 POST /messages"
assert_contains "$README" '/healthz' "README 记录了 GET /healthz"

# ------------------------------------------------------- 8. README 内容要件

section "8. README 内容要件"

for kw in \
	'无回答持久化' \
	'无幂等' \
	'无对话上下文' \
	'不支持附件' \
	'跨换行' \
	'不可更正' \
	'明文' \
	'模型故障不影响认证' \
	'只能保证' \
	'不做向量检索'; do
	assert_contains "$README" "$kw" "README 已知限制覆盖 “$kw”"
done

assert_contains "$README" '2026-11.jsonl' "README 给出了 monthly 分片文件名"
assert_contains "$README" '2026-W45.jsonl' "README 给出了 weekly 分片文件名"
assert_contains "$README" '2026-11-04.jsonl' "README 给出了 daily 分片文件名"

assert_contains "$README" 'docker build' "README 给了 docker build 命令"
assert_contains "$README" 'docker run' "README 给了 docker run 命令"
assert_contains "$README" 'curl' "README 给了 curl 示例"

# README 不得把 spec 明确放弃的能力描述成已实现。
assert_no_claim "$README" '回答会保存' "README 未把 spec 放弃的能力说成已实现（回答持久化）"
assert_no_claim "$README" '支持幂等' "README 未把 spec 放弃的能力说成已实现（幂等）"
assert_no_claim "$README" '支持附件' "README 未把 spec 放弃的能力说成已实现（附件）"
assert_no_claim "$README" '支持多轮追问' "README 未把 spec 放弃的能力说成已实现（多轮追问）"
assert_no_claim "$README" '支持向量检索' "README 未把 spec 放弃的能力说成已实现（向量检索）"
assert_no_claim "$README" '支持对话上下文' "README 未把 spec 放弃的能力说成已实现（会话上下文）"

# -------------------------------------------- 9. 认证迁移与部署责任
section "9. 认证迁移、隐私与部署责任"
for kw in 'SHIYI_AUTH_TOKEN' 'auth_token_env' '7 天' '30 天' '同一密码重新哈希' '0700' '0600' 'HTTPS' 'CSRF' '加密备份' '管理员' '上游模型' '不是数据加密'; do
    assert_contains "$README" "$kw" "README说明认证/隐私边界 $kw"
done
for route in /auth/session /auth/login /auth/logout /auth/logout-all; do
    assert_contains "$README" "$route" "README记录认证路由 $route"
done
assert_absent "$README" '启用鉴权时 Web 页面不可用' "不再宣传旧Web不可用行为"
assert_absent "$README" '重启后没有任何会话状态' "区分持久登录与对话上下文"
assert_contains .github/workflows/ci.yml 'make check' "CI运行完整本地门禁"
assert_contains .github/workflows/ci.yml 'make image-check' "CI运行真实安全容器门禁"
assert_contains docs/deployment.md '回滚旧认证状态' "部署说明禁止用旧快照恢复撤销资格"

# ------------------------------------------------------------------ 汇总

printf '\n----------------------------------------\n'
if [ "$FAILURES" -eq 0 ]; then
	printf '全部通过：%d 项检查。\n' "$CHECKS"
	exit 0
fi
printf '失败 %d / %d 项检查。\n' "$FAILURES" "$CHECKS"
exit 1
