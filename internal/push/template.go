package push

import (
	"fmt"
	"regexp"

	"github.com/starlink/push/internal/domain"
)

var varPattern = regexp.MustCompile(`\{\{\s*([a-zA-Z0-9_]+)\s*\}\}`)

// VarPattern 暴露模板变量正则，供预览/校验复用
func VarPattern() *regexp.Regexp { return varPattern }

// RenderTemplate 渲染个性化变量 {{name}}（兼容：有 vars 时缺失→空串；无 vars 时保留占位符）
func RenderTemplate(body string, vars map[string]string) string {
	if len(vars) == 0 {
		return body
	}
	out, _ := RenderTemplateWithPolicy(body, vars, domain.MissingVarEmpty, nil)
	return out
}

// RenderTemplateWithPolicy 按缺失变量策略渲染。
// policy=error 且存在未解析变量时返回 error；defaults 用于 default 策略。
func RenderTemplateWithPolicy(body string, vars map[string]string, policy domain.MissingVarPolicy, defaults map[string]string) (string, error) {
	policy = policy.Normalize()
	if vars == nil && policy == domain.MissingVarEmpty {
		// 历史兼容：nil vars + empty → 保留占位符
		return body, nil
	}
	if vars == nil {
		vars = map[string]string{}
	}
	var missing []string
	out := varPattern.ReplaceAllStringFunc(body, func(m string) string {
		sub := varPattern.FindStringSubmatch(m)
		if len(sub) < 2 {
			return m
		}
		name := sub[1]
		if v, ok := vars[name]; ok {
			return v
		}
		switch policy {
		case domain.MissingVarKeep:
			return m
		case domain.MissingVarDefault:
			if defaults != nil {
				if d, ok := defaults[name]; ok {
					return d
				}
			}
			return ""
		case domain.MissingVarError:
			missing = append(missing, name)
			return ""
		default: // empty
			return ""
		}
	})
	if policy == domain.MissingVarError && len(missing) > 0 {
		return out, fmt.Errorf("missing template vars: %v", missing)
	}
	return out, nil
}

// MissingVars 列出 body 中出现但 vars 未提供的变量名（去重）
func MissingVars(body string, vars map[string]string) []string {
	seen := map[string]struct{}{}
	var missing []string
	for _, m := range varPattern.FindAllStringSubmatch(body, -1) {
		if len(m) < 2 {
			continue
		}
		k := m[1]
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		if vars != nil {
			if _, ok := vars[k]; ok {
				continue
			}
		}
		missing = append(missing, k)
	}
	return missing
}
