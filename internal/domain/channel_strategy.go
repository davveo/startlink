package domain

import (
	"encoding/json"
	"strconv"
	"strings"
)

// ChannelRouteRule 条件路由规则（按序匹配，第一条命中生效；无 When 为默认兜底）
type ChannelRouteRule struct {
	When     *RouteCondition `json:"when,omitempty"`
	Channels []ChannelType   `json:"channels"`
}

// RouteCondition 用户变量/扩展字段条件
type RouteCondition struct {
	Var   string `json:"var"`             // 先查 vars，再查 extra
	Op    string `json:"op"`              // eq|ne|in|gt|gte|lt|lte|exists|not_exists
	Value string `json:"value,omitempty"` // in 时用逗号分隔
}

// MatchRouteRules 按规则解析渠道；无命中返回 fallback 渠道链
func MatchRouteRules(rules []ChannelRouteRule, vars map[string]string, extra map[string]any, fallback []ChannelType) []ChannelType {
	if len(rules) == 0 {
		return fallback
	}
	var defaultChs []ChannelType
	for _, r := range rules {
		if r.When == nil {
			if len(r.Channels) > 0 {
				defaultChs = r.Channels
			}
			continue
		}
		if r.When.Match(vars, extra) && len(r.Channels) > 0 {
			return r.Channels
		}
	}
	if len(defaultChs) > 0 {
		return defaultChs
	}
	return fallback
}

// Match 评估条件
func (c *RouteCondition) Match(vars map[string]string, extra map[string]any) bool {
	if c == nil {
		return true
	}
	op := strings.ToLower(strings.TrimSpace(c.Op))
	if op == "" {
		op = "eq"
	}
	raw, ok := lookupRouteValue(c.Var, vars, extra)
	switch op {
	case "exists":
		return ok && strings.TrimSpace(raw) != ""
	case "not_exists":
		return !ok || strings.TrimSpace(raw) == ""
	}
	if !ok {
		return false
	}
	switch op {
	case "eq", "==":
		return raw == c.Value
	case "ne", "!=":
		return raw != c.Value
	case "in":
		for _, p := range strings.Split(c.Value, ",") {
			if strings.TrimSpace(p) == raw {
				return true
			}
		}
		return false
	case "gt", "gte", "lt", "lte":
		rv, err1 := strconv.ParseFloat(raw, 64)
		cv, err2 := strconv.ParseFloat(c.Value, 64)
		if err1 != nil || err2 != nil {
			// 回退字符串比较
			switch op {
			case "gt":
				return raw > c.Value
			case "gte":
				return raw >= c.Value
			case "lt":
				return raw < c.Value
			case "lte":
				return raw <= c.Value
			}
		}
		switch op {
		case "gt":
			return rv > cv
		case "gte":
			return rv >= cv
		case "lt":
			return rv < cv
		case "lte":
			return rv <= cv
		}
	}
	return false
}

func lookupRouteValue(key string, vars map[string]string, extra map[string]any) (string, bool) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", false
	}
	if vars != nil {
		if v, ok := vars[key]; ok {
			return v, true
		}
	}
	if extra != nil {
		if v, ok := extra[key]; ok {
			return stringifyRouteVal(v), true
		}
	}
	return "", false
}

func stringifyRouteVal(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case bool:
		if t {
			return "true"
		}
		return "false"
	case json.Number:
		return t.String()
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return ""
		}
		return string(b)
	}
}

// DefaultChannelCosts 默认渠道相对成本（越低越优先）
var DefaultChannelCosts = map[ChannelType]int{
	ChannelInbox:    1,
	ChannelAppPush:  2,
	ChannelEmail:    3,
	ChannelWecom:    4,
	ChannelDingtalk: 4,
	ChannelSMS:      10,
}

// SortChannelsByCost 按成本升序（成本相同保持相对顺序）
func SortChannelsByCost(chs []ChannelType, costs map[ChannelType]int) []ChannelType {
	if len(chs) <= 1 {
		return chs
	}
	type item struct {
		ch    ChannelType
		cost  int
		index int
	}
	items := make([]item, len(chs))
	for i, ch := range chs {
		c := 100
		if costs != nil {
			if v, ok := costs[ch]; ok {
				c = v
			}
		} else if v, ok := DefaultChannelCosts[ch]; ok {
			c = v
		}
		items[i] = item{ch: ch, cost: c, index: i}
	}
	// stable-ish insertion by cost then index
	out := make([]ChannelType, len(items))
	used := make([]bool, len(items))
	for n := 0; n < len(items); n++ {
		best := -1
		for i := range items {
			if used[i] {
				continue
			}
			if best < 0 || items[i].cost < items[best].cost ||
				(items[i].cost == items[best].cost && items[i].index < items[best].index) {
				best = i
			}
		}
		used[best] = true
		out[n] = items[best].ch
	}
	return out
}

// ParseChannelRoutesJSON 解析条件路由
func ParseChannelRoutesJSON(raw string) []ChannelRouteRule {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" || raw == "[]" {
		return nil
	}
	var rules []ChannelRouteRule
	if err := json.Unmarshal([]byte(raw), &rules); err != nil {
		return nil
	}
	return rules
}

// ParseChannelCostsJSON 解析渠道成本
func ParseChannelCostsJSON(raw string) map[ChannelType]int {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" || raw == "{}" {
		return nil
	}
	var m map[string]int
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil
	}
	out := make(map[ChannelType]int, len(m))
	for k, v := range m {
		out[ChannelType(k)] = v
	}
	return out
}

func MarshalChannelRoutes(rules []ChannelRouteRule) *string {
	return MarshalJSONColumn(rules, true)
}

func MarshalChannelCosts(costs map[ChannelType]int) *string {
	if len(costs) == 0 {
		return MarshalJSONColumn(map[string]int{}, false)
	}
	m := make(map[string]int, len(costs))
	for k, v := range costs {
		m[string(k)] = v
	}
	return MarshalJSONColumn(m, false)
}
