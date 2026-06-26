package config

import "strings"

// ParseDotenv は .env テキストを key=value のマップに展開する。
func ParseDotenv(text string) map[string]string {
	out := map[string]string{}
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSuffix(raw, "\r")
		line = trimExportPrefix(line)
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		eq := strings.Index(line, "=")
		if eq == -1 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		value := strings.TrimSpace(line[eq+1:])
		if len(value) >= 2 {
			if (value[0] == '"' && value[len(value)-1] == '"') ||
				(value[0] == '\'' && value[len(value)-1] == '\'') {
				value = value[1 : len(value)-1]
			}
		}
		out[key] = value
	}
	return out
}

// trimExportPrefix は行頭の `export ` を取り除く（先頭の空白も許容）。
func trimExportPrefix(line string) string {
	trimmed := strings.TrimLeft(line, " \t")
	const prefix = "export "
	if strings.HasPrefix(trimmed, prefix) {
		// 元の行頭空白は捨てて、export 以降を返す。
		return strings.TrimLeft(trimmed[len(prefix):], " \t")
	}
	return line
}
