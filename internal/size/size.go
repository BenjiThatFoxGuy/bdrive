package size

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

type Size int64

var sizeSuffixes = map[string]float64{
	"":    1,
	"B":   1,
	"K":   1 << 10,
	"KB":  1 << 10,
	"KIB": 1 << 10,
	"M":   1 << 20,
	"MB":  1 << 20,
	"MIB": 1 << 20,
	"G":   1 << 30,
	"GB":  1 << 30,
	"GIB": 1 << 30,
	"T":   1 << 40,
	"TB":  1 << 40,
	"TIB": 1 << 40,
}

func ParseSize(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, fmt.Errorf("empty size")
	}

	numberEnd := 0
	for numberEnd < len(value) {
		c := value[numberEnd]
		if (c >= '0' && c <= '9') || c == '.' {
			numberEnd++
			continue
		}
		break
	}
	if numberEnd == 0 {
		return 0, fmt.Errorf("invalid size %q", value)
	}

	number, err := strconv.ParseFloat(value[:numberEnd], 64)
	if err != nil {
		return 0, err
	}
	if number < 0 {
		return 0, fmt.Errorf("size must be non-negative")
	}

	suffix := strings.ToUpper(strings.TrimSpace(value[numberEnd:]))
	multiplier, ok := sizeSuffixes[suffix]
	if !ok {
		return 0, fmt.Errorf("unknown size suffix %q", suffix)
	}

	bytes := number * multiplier
	if bytes > math.MaxInt64 {
		return 0, fmt.Errorf("size overflows int64")
	}
	return int64(bytes), nil
}

func (s *Size) Set(value string) error {
	parsed, err := ParseSize(value)
	if err != nil {
		return err
	}
	*s = Size(parsed)
	return nil
}

func (s Size) String() string {
	bytes := int64(s)
	units := []struct {
		suffix string
		value  int64
	}{
		{"TB", 1 << 40},
		{"GB", 1 << 30},
		{"MB", 1 << 20},
		{"KB", 1 << 10},
	}
	for _, unit := range units {
		if bytes >= unit.value && bytes%unit.value == 0 {
			return strconv.FormatInt(bytes/unit.value, 10) + unit.suffix
		}
	}
	return strconv.FormatInt(bytes, 10) + "B"
}

func (s *Size) UnmarshalText(text []byte) error {
	return s.Set(string(text))
}

func (s *Size) Type() string {
	return "Size"
}
