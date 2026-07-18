package httpapi

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// flexibleInt preserves the old CMS contract, where form and database values
// may be serialized as either JSON numbers or quoted decimal strings.
type flexibleInt int

func (i *flexibleInt) UnmarshalJSON(data []byte) error {
	raw := strings.TrimSpace(string(data))
	if raw == "" {
		return fmt.Errorf("empty integer")
	}
	if raw == "null" {
		*i = 0
		return nil
	}

	if strings.HasPrefix(raw, `"`) {
		var value string
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
		raw = strings.TrimSpace(value)
	}

	value, err := strconv.Atoi(raw)
	if err != nil {
		return fmt.Errorf("invalid integer %q: %w", raw, err)
	}

	*i = flexibleInt(value)
	return nil
}

func flexibleIntPtr(value *flexibleInt) *int {
	if value == nil {
		return nil
	}

	converted := int(*value)
	return &converted
}

// flexibleBool accepts native JSON booleans and the "true"/"false" strings
// commonly produced by HTML forms and the existing CMS middleware.
type flexibleBool bool

func (b *flexibleBool) UnmarshalJSON(data []byte) error {
	raw := strings.TrimSpace(string(data))
	if raw == "null" {
		*b = false
		return nil
	}

	if strings.HasPrefix(raw, `"`) {
		var value string
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
		raw = strings.TrimSpace(value)
	}

	value, err := strconv.ParseBool(raw)
	if err != nil {
		return fmt.Errorf("invalid boolean %q: %w", raw, err)
	}

	*b = flexibleBool(value)
	return nil
}
