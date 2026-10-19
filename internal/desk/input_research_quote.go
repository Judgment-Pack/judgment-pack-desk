package desk

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// researchSourceQuote allows layout whitespace to differ, never words,
// punctuation or case. Return the exact retained bytes, not the model's
// reformatted quotation; the original action remains in the attempt journal.
func researchSourceQuote(source, quote string) (string, bool) {
	if len(quote) == 0 || len(quote) > 2000 || !utf8.ValidString(quote) || !utf8.ValidString(source) {
		return "", false
	}
	needle := strings.Join(strings.Fields(quote), " ")
	if needle == "" {
		return "", false
	}
	if strings.Contains(source, quote) {
		return quote, true
	}

	// Each normalized byte retains its original span. A collapsed whitespace
	// run maps to the whole run, including PDF line breaks and nonbreaking spaces.
	var normalized strings.Builder
	starts, ends := make([]int, 0, len(source)), make([]int, 0, len(source))
	space := false
	for index, r := range source {
		size := utf8.RuneLen(r)
		if unicode.IsSpace(r) {
			if space {
				ends[len(ends)-1] = index + size
				continue
			}
			normalized.WriteByte(' ')
			starts, ends = append(starts, index), append(ends, index+size)
			space = true
			continue
		}
		normalized.WriteString(source[index : index+size])
		for offset := 0; offset < size; offset++ {
			starts, ends = append(starts, index+offset), append(ends, index+offset+1)
		}
		space = false
	}
	text := normalized.String()
	for offset := 0; offset <= len(text)-len(needle); {
		index := strings.Index(text[offset:], needle)
		if index < 0 {
			break
		}
		index += offset
		start, end := starts[index], ends[index+len(needle)-1]
		if end-start <= 2000 {
			return source[start:end], true
		}
		offset = index + 1
	}
	return "", false
}
