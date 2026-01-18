package handlers

import (
	"fmt"
	"html/template"
	"strings"
)

func interfaceToInt64(v interface{}) int64 {
	switch val := v.(type) {
	case int64:
		return val
	case int:
		return int64(val)
	default:
		return 0
	}
}

func GetFuncMap() template.FuncMap {
	return template.FuncMap{
		"hasPrefix": strings.HasPrefix,
		"split":     strings.Split,
		"contains":  strings.Contains,
		"formatSize": func(size int64) string {
			if size == 0 {
				return "0 B"
			}
			units := []string{"B", "KB", "MB", "GB", "TB"}
			i := 0
			fSize := float64(size)
			for fSize >= 1024 && i < len(units)-1 {
				fSize /= 1024
				i++
			}
			return fmt.Sprintf("%.2f %s", fSize, units[i])
		},
	}
}
