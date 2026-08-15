package credential

import (
	"fmt"
	"strings"
)

type Ref string

const targetPrefix = "AggregationHub/"

func (ref Ref) Validate() error {
	value := string(ref)
	if value == "" || len(value) > 128 || strings.ContainsAny(value, "\\\x00") {
		return ErrInvalidReference
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-' || r == ':' || r == '/') {
			return fmt.Errorf("%w: 非法字符", ErrInvalidReference)
		}
	}
	return nil
}
func (ref Ref) targetName() (string, error) {
	if err := ref.Validate(); err != nil {
		return "", err
	}
	return targetPrefix + string(ref), nil
}
