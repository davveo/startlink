package domain

import (
	"encoding/json"
	"fmt"
	"strings"
)

// MissingVarPolicy 模板缺失变量策略
type MissingVarPolicy string

const (
	MissingVarError   MissingVarPolicy = "error"   // 缺变量时报错
	MissingVarKeep    MissingVarPolicy = "keep"    // 保留 {{var}} 字面量
	MissingVarDefault MissingVarPolicy = "default" // 用 schema 默认值，再无则空串
	MissingVarEmpty   MissingVarPolicy = "empty"   // 替换为空串（历史默认）
)

func (p MissingVarPolicy) Normalize() MissingVarPolicy {
	switch p {
	case MissingVarError, MissingVarKeep, MissingVarDefault, MissingVarEmpty:
		return p
	case "":
		return MissingVarEmpty
	default:
		return MissingVarEmpty
	}
}

func (p MissingVarPolicy) Valid() bool {
	switch p {
	case "", MissingVarError, MissingVarKeep, MissingVarDefault, MissingVarEmpty:
		return true
	default:
		return false
	}
}

// ChannelContent 单渠道模板内容
type ChannelContent struct {
	Title string         `json:"title,omitempty"`
	Body  string         `json:"body,omitempty"`
	Extra map[string]any `json:"extra,omitempty"`
}

// LocaleContent 某语言下的正文与分渠道内容
type LocaleContent struct {
	Body     string                    `json:"body,omitempty"`
	Contents map[string]ChannelContent `json:"contents,omitempty"`
}

// VarDef 模板变量声明
type VarDef struct {
	Name      string `json:"name"`
	Type      string `json:"type,omitempty"` // string | number | bool
	Required  bool   `json:"required,omitempty"`
	Default   string `json:"default,omitempty"`
	Example   string `json:"example,omitempty"`
	Sensitive bool   `json:"sensitive,omitempty"`
}

// ParseContentsJSON 解析 contents JSON；空/非法返回空 map
func ParseContentsJSON(raw string) map[string]ChannelContent {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" || raw == "{}" {
		return nil
	}
	var m map[string]ChannelContent
	if err := json.Unmarshal([]byte(raw), &m); err != nil || len(m) == 0 {
		return nil
	}
	return m
}

// ParseVarSchemaJSON 解析 var_schema
func ParseVarSchemaJSON(raw string) []VarDef {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" || raw == "[]" {
		return nil
	}
	var list []VarDef
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return nil
	}
	return list
}

// ParseLocalesJSON 解析 locales
func ParseLocalesJSON(raw string) map[string]LocaleContent {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" || raw == "{}" {
		return nil
	}
	var m map[string]LocaleContent
	if err := json.Unmarshal([]byte(raw), &m); err != nil || len(m) == 0 {
		return nil
	}
	return m
}

// MarshalJSONColumn 序列化 JSON 列；空值返回 "{}" / "[]" 指针，避免 MySQL Error 3140
func MarshalJSONColumn(v any, emptyArray bool) *string {
	if v == nil {
		s := "{}"
		if emptyArray {
			s = "[]"
		}
		return &s
	}
	b, err := json.Marshal(v)
	if err != nil {
		s := "{}"
		if emptyArray {
			s = "[]"
		}
		return &s
	}
	s := string(b)
	if s == "null" {
		s = "{}"
		if emptyArray {
			s = "[]"
		}
	}
	return &s
}

// JSONColumnValue 读取 *string JSON 列，空则返回 empty
func JSONColumnValue(p *string, empty string) string {
	if p == nil || strings.TrimSpace(*p) == "" || *p == "null" {
		return empty
	}
	return *p
}

// TemplateHasBody 正文或任一分渠道 body 非空
func TemplateHasBody(body string, contents map[string]ChannelContent) bool {
	if strings.TrimSpace(body) != "" {
		return true
	}
	for _, c := range contents {
		if strings.TrimSpace(c.Body) != "" {
			return true
		}
	}
	return false
}

// ResolveLocaleContent 用户 locale → default_locale → 根 body/contents
func ResolveLocaleContent(rootBody string, rootContents map[string]ChannelContent, defaultLocale string, locales map[string]LocaleContent, userLocale string) (body string, contents map[string]ChannelContent) {
	body = rootBody
	contents = rootContents
	try := func(loc string) bool {
		if loc == "" || locales == nil {
			return false
		}
		lc, ok := locales[loc]
		if !ok {
			return false
		}
		if strings.TrimSpace(lc.Body) != "" || len(lc.Contents) > 0 {
			if strings.TrimSpace(lc.Body) != "" {
				body = lc.Body
			}
			if len(lc.Contents) > 0 {
				contents = lc.Contents
			}
			return true
		}
		return false
	}
	if try(userLocale) {
		return body, contents
	}
	if defaultLocale != "" && defaultLocale != userLocale {
		try(defaultLocale)
	}
	return body, contents
}

// ResolveChannelContent 按渠道取 title/body/extra；缺省回退 rootBody + campaignTitle
func ResolveChannelContent(ch ChannelType, campaignTitle, rootBody string, contents map[string]ChannelContent) (title, body string, extra map[string]any) {
	title = campaignTitle
	body = rootBody
	if contents != nil {
		if c, ok := contents[string(ch)]; ok {
			if strings.TrimSpace(c.Title) != "" {
				title = c.Title
			}
			if strings.TrimSpace(c.Body) != "" {
				body = c.Body
			}
			if len(c.Extra) > 0 {
				extra = c.Extra
			}
		}
	}
	return title, body, extra
}

// ValidateVarsAgainstSchema 按 var_schema 校验；返回补齐默认值后的 vars 与错误列表
func ValidateVarsAgainstSchema(schema []VarDef, vars map[string]string) (map[string]string, []string) {
	out := map[string]string{}
	for k, v := range vars {
		out[k] = v
	}
	var errs []string
	for _, def := range schema {
		name := strings.TrimSpace(def.Name)
		if name == "" {
			continue
		}
		v, ok := out[name]
		if !ok || v == "" {
			if def.Default != "" {
				out[name] = def.Default
				v = def.Default
				ok = true
			}
		}
		if def.Required && (!ok || v == "") {
			errs = append(errs, fmt.Sprintf("required var %q missing", name))
			continue
		}
		if !ok || v == "" {
			continue
		}
		switch strings.ToLower(def.Type) {
		case "", "string":
			// ok
		case "number":
			if _, err := parseLooseNumber(v); err != nil {
				errs = append(errs, fmt.Sprintf("var %q expect number", name))
			}
		case "bool":
			lv := strings.ToLower(v)
			if lv != "true" && lv != "false" && lv != "1" && lv != "0" {
				errs = append(errs, fmt.Sprintf("var %q expect bool", name))
			}
		}
	}
	return out, errs
}

func parseLooseNumber(s string) (float64, error) {
	var f float64
	_, err := fmt.Sscanf(strings.TrimSpace(s), "%f", &f)
	return f, err
}

// DefaultsFromSchema 提取 schema 默认值 map
func DefaultsFromSchema(schema []VarDef) map[string]string {
	out := map[string]string{}
	for _, def := range schema {
		if def.Name != "" && def.Default != "" {
			out[def.Name] = def.Default
		}
	}
	return out
}
