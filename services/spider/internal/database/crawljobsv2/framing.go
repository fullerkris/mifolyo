package crawljobsv2

import (
	"encoding/binary"
	"errors"
)

var (
	ErrInvalidFieldName    = errors.New("crawljobsv2: invalid record field name")
	ErrDuplicateFieldName  = errors.New("crawljobsv2: duplicate record field name")
	ErrInvalidSectionLabel = errors.New("crawljobsv2: invalid section label")
)

// Field is one ordered RECORD field. Value is binary-safe; callers that model
// protocol text are responsible for validating UTF-8 before framing it.
type Field struct {
	Name  string
	Value []byte
}

// Record is an ordered collection of named fields.
type Record []Field

// U64 encodes an unsigned integer in protocol big-endian form.
func U64(value uint64) []byte {
	encoded := make([]byte, 8)
	binary.BigEndian.PutUint64(encoded, value)
	return encoded
}

// F length-prefixes exact bytes using U64.
func F(value []byte) []byte {
	framed := make([]byte, 8+len(value))
	binary.BigEndian.PutUint64(framed[:8], uint64(len(value)))
	copy(framed[8:], value)
	return framed
}

// EncodeRecord implements RECORD(fields...) exactly.
func EncodeRecord(record Record) ([]byte, error) {
	encoded := U64(uint64(len(record)))
	seen := make(map[string]struct{}, len(record))
	for _, field := range record {
		if field.Name == "" || !isPrintableASCII(field.Name) {
			return nil, ErrInvalidFieldName
		}
		if _, duplicate := seen[field.Name]; duplicate {
			return nil, ErrDuplicateFieldName
		}
		seen[field.Name] = struct{}{}
		encoded = append(encoded, F([]byte(field.Name))...)
		encoded = append(encoded, F(field.Value)...)
	}
	return encoded, nil
}

// EncodeSection implements SECTION(label, records...) exactly, including the F
// around each encoded RECORD.
func EncodeSection(label string, records []Record) ([]byte, error) {
	if label == "" || !isPrintableASCII(label) {
		return nil, ErrInvalidSectionLabel
	}
	encoded := F([]byte(label))
	encoded = append(encoded, U64(uint64(len(records)))...)
	for _, record := range records {
		encodedRecord, err := EncodeRecord(record)
		if err != nil {
			return nil, err
		}
		encoded = append(encoded, F(encodedRecord)...)
	}
	return encoded, nil
}

func isPrintableASCII(value string) bool {
	for index := range value {
		if value[index] < 0x20 || value[index] > 0x7e {
			return false
		}
	}
	return true
}
