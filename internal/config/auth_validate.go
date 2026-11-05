package config

import (
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

var errAuthMigration = errors.New("旧 Bearer 配置已移除，请迁移到 auth.password_hash 和 auth.public_origin；删除 auth_token_env 与 SHIYI_AUTH_TOKEN")

func (c Config) validateAuth() []string {
	var problems []string
	if _, set := os.LookupEnv(LegacyAuthTokenEnv); set || c.AuthTokenEnv != "" {
		problems = append(problems, errAuthMigration.Error())
	}
	a := c.Auth
	u, err := url.Parse(a.PublicOrigin)
	valid := err == nil && u.Host != "" && u.Hostname() != "" && u.User == nil && u.Path == "" && u.RawQuery == "" && !u.ForceQuery && u.Fragment == "" && !strings.ContainsAny(a.PublicOrigin, " \t\r\n,#?\\") && !strings.HasSuffix(u.Host, ":") && (u.Scheme == "https" || (a.DevAnonymous && u.Scheme == "http"))
	if valid && u.Port() != "" {
		n, e := strconv.Atoi(u.Port())
		valid = e == nil && n >= 1 && n <= 65535
	}
	if !valid {
		problems = append(problems, "auth.public_origin 必须是唯一且无路径的 HTTPS 来源（开发匿名可用 HTTP）")
	}
	if !a.DevAnonymous {
		if strings.TrimSpace(a.PasswordHash) == "" {
			problems = append(problems, "auth.password_hash 不能为空；请配置 bcrypt 密码哈希")
		}
		if strings.TrimSpace(a.StateDir) == "" {
			problems = append(problems, "auth.state_dir 不能为空")
		} else {
			memory, e1 := resolvedPath(c.DataDir)
			state, e2 := resolvedPath(a.StateDir)
			if e1 != nil || e2 != nil {
				problems = append(problems, "认证与记忆目录路径无法安全解析")
			} else if within(memory, state) || within(state, memory) {
				problems = append(problems, "auth.state_dir 必须与 data_dir 分离，不能相互包含")
			}
		}
	}
	return problems
}
func within(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

// Resolve existing symlink ancestors even when the requested directory has not
// been created yet. Comparing cleaned strings alone allows an alias bypass.
func resolvedPath(path string) (string, error) {
	path, err := ExpandHome(path)
	if err != nil {
		return "", err
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return "", err
	}
	var suffix []string
	for {
		resolved, err := filepath.EvalSymlinks(path)
		if err == nil {
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			return resolved, nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(path)
		if parent == path {
			return "", err
		}
		suffix = append(suffix, filepath.Base(path))
		path = parent
	}
}
