package security

// IsRFC4122UUID reports whether value is a canonical hyphenated RFC 4122 UUID
// with a supported version and RFC 4122 variant.
func IsRFC4122UUID(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	for index, character := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			continue
		}
		if !isHex(character) {
			return false
		}
	}
	version := value[14]
	if version < '1' || version > '5' {
		return false
	}
	variant := value[19]
	return variant == '8' || variant == '9' || variant == 'a' || variant == 'A' || variant == 'b' || variant == 'B'
}

func isHex(value rune) bool {
	return value >= '0' && value <= '9' || value >= 'a' && value <= 'f' || value >= 'A' && value <= 'F'
}
