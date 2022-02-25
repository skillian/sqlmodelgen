package sqlmodelgen

import "strings"

func englishPluralize(noun string) string {
	if strings.HasSuffix(noun, "s") ||
		strings.HasSuffix(noun, "x") ||
		strings.HasSuffix(noun, "z") {
		return noun + "es"
	}
	return noun + "s"
}
