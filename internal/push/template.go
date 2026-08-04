package push

import (
	"regexp"
)

var varPattern = regexp.MustCompile(`\{\{\s*([a-zA-Z0-9_]+)\s*\}\}`)

// RenderTemplate 渲染个性化变量 {{name}}
func RenderTemplate(body string, vars map[string]string) string {
	if len(vars) == 0 {
		return body
	}
	return varPattern.ReplaceAllStringFunc(body, func(m string) string {
		sub := varPattern.FindStringSubmatch(m)
		if len(sub) < 2 {
			return m
		}
		if v, ok := vars[sub[1]]; ok {
			return v
		}
		return ""
	})
}
