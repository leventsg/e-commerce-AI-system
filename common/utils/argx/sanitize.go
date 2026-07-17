package argx

import (
	"reflect"
	"strings"
	"unicode"
)

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
	return sanitizeReflectValue(reflect.ValueOf(value), banned)
}

// 递归清理反射值中的敏感键值对
func sanitizeReflectValue(value reflect.Value, banned map[string]bool) any {
	if !value.IsValid() {
		return nil
	}
	for value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return nil
		}
		value = value.Elem()
	}
	switch value.Kind() {
	case reflect.Map:
		if value.Type().Key().Kind() != reflect.String {
			return value.Interface()
		}
		cleaned := make(map[string]any, value.Len())
		iterator := value.MapRange()
		for iterator.Next() {
			key := iterator.Key().String()
			if banned[normalizeKey(key)] {
				continue
			}
			cleaned[key] = sanitizeReflectValue(iterator.Value(), banned)
		}
		return cleaned
	case reflect.Slice, reflect.Array:
		if value.Type().Elem().Kind() == reflect.Uint8 {
			return value.Interface()
		}
		cleaned := make([]any, 0, value.Len())
		for i := 0; i < value.Len(); i++ {
			cleaned = append(cleaned, sanitizeReflectValue(value.Index(i), banned))
		}
		return cleaned
	default:
		return value.Interface()
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

// 忽略大小写和常见分隔符，统一 snake_case、kebab-case 与 camelCase 键。
func normalizeKey(key string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, strings.TrimSpace(key))
}
