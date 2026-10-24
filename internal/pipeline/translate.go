package pipeline

import "encoding/json"

// ============================================================
// translate 步骤：按数据字典翻译 JSON key 为中文
// ============================================================

func (ctx *TaskContext) runTranslate() error {
	dict := ctx.Cfg.Dict
	if len(dict) == 0 {
		// 无字典时，翻译后 JSON = 原始 JSON
		ctx.TranslatedJSON = ctx.RawBodyStr
		return nil
	}

	translated := translateJSON(ctx.RawBody, dict)
	ctx.TranslatedJSON = mustJSON(translated)
	return nil
}

// translateJSON 将 JSON 中出现的 key 按 dict 翻译为中文，递归处理嵌套结构
func translateJSON(v interface{}, dict map[string]string) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(val))
		for k, vv := range val {
			newKey := k
			if cn, ok := dict[k]; ok && cn != "" {
				newKey = cn
			}
			out[newKey] = translateJSON(vv, dict)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(val))
		for i, item := range val {
			out[i] = translateJSON(item, dict)
		}
		return out
	default:
		return v
	}
}

// mustJSON 序列化 JSON，失败返回 {}
func mustJSON(v interface{}) string {
	if v == nil {
		return "{}"
	}
	data, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(data)
}