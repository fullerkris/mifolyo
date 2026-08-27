// Package strictjson rejects ambiguous JSON before decoding it into a contract.
package strictjson

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"unicode/utf8"
)

// Decode accepts exactly one UTF-8 JSON value, rejects duplicate and unknown
// object members, and decodes it into destination.
func Decode(data []byte, destination any) error {
	if destination == nil {
		return fmt.Errorf("destination is required")
	}
	if !utf8.Valid(data) {
		return fmt.Errorf("invalid UTF-8")
	}
	if err := validateSingleValue(data); err != nil {
		return err
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	return nil
}

func validateSingleValue(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := consumeValue(decoder, "$"); err != nil {
		return err
	}
	if token, err := decoder.Token(); err == io.EOF {
		return nil
	} else if err != nil {
		return fmt.Errorf("trailing JSON: %w", err)
	} else {
		return fmt.Errorf("trailing JSON value beginning with %v", token)
	}
}

func consumeValue(decoder *json.Decoder, path string) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, isDelimiter := token.(json.Delim)
	if !isDelimiter {
		return nil
	}

	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("object member at %s is not a string", path)
			}
			if _, duplicate := seen[key]; duplicate {
				return fmt.Errorf("duplicate object member %q at %s", key, path)
			}
			seen[key] = struct{}{}
			if err := consumeValue(decoder, path+"."+key); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil {
			return err
		}
		if closing != json.Delim('}') {
			return fmt.Errorf("object at %s is not closed", path)
		}
		return nil
	case '[':
		for index := 0; decoder.More(); index++ {
			if err := consumeValue(decoder, fmt.Sprintf("%s[%d]", path, index)); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil {
			return err
		}
		if closing != json.Delim(']') {
			return fmt.Errorf("array at %s is not closed", path)
		}
		return nil
	default:
		return fmt.Errorf("unexpected JSON delimiter %q at %s", delimiter, path)
	}
}
