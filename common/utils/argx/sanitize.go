package argx

import "strings"

// 清理输入数据中的敏感键值对
func SanitizeMapKeys(input map[string]any, bannedKeys []string) map[string]any {
	if len(input) == 0 {
		return map[string]any{}
	}
	// 生成一个规范化的禁止键集合，方便后续检查
	banned := normalizedKeySet(bannedKeys)
	cleaned := make(map[string]any, len(input))
	for key, value := range input {
		if banned[normalizeKey(key)] {
			continue
		}
		cleaned[key] = sanitizeValue(value, banned)
	}
	return cleaned
}

func sanitizeValue(value any, banned map[string]bool) any {
	switch typed := value.(type) {
	case map[string]any:
		cleaned := make(map[string]any, len(typed))
		for key, nested := range typed {
			if banned[normalizeKey(key)] {
				continue
			}
			cleaned[key] = sanitizeValue(nested, banned)
		}
		return cleaned
	case []any:
		cleaned := make([]any, 0, len(typed))
		for _, item := range typed {
			cleaned = append(cleaned, sanitizeValue(item, banned))
		}
		return cleaned
	default:
		return typed
	}
}

// 将禁止键列表转换为一个规范化的键集合
func normalizedKeySet(keys []string) map[string]bool {
	result := make(map[string]bool, len(keys))
	for _, key := range keys {
		normalized := normalizeKey(key)
		if normalized != "" {
			result[normalized] = true
		}
	}
	return result
}

// 去除字符串中的空格并转换为小写
func normalizeKey(key string) string {
	return strings.ToLower(strings.TrimSpace(key))
}
