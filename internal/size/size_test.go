package size

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseSize(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  int64
	}{
		{name: "bytes without suffix", value: "512", want: 512},
		{name: "bytes suffix", value: "512B", want: 512},
		{name: "kilobytes", value: "5KB", want: 5 << 10},
		{name: "kibibytes", value: "5KiB", want: 5 << 10},
		{name: "megabytes", value: "32MB", want: 32 << 20},
		{name: "lowercase gigabytes", value: "5gb", want: 5 << 30},
		{name: "terabytes", value: "2TB", want: 2 << 40},
		{name: "fractional megabytes", value: "1.5MB", want: int64(1.5 * float64(1<<20))},
		{name: "spaced suffix", value: "4 GB", want: 4 << 30},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseSize(tt.value)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseSizeInvalid(t *testing.T) {
	for _, value := range []string{"", "GB", "1XB", "-1MB"} {
		t.Run(value, func(t *testing.T) {
			_, err := ParseSize(value)
			assert.Error(t, err)
		})
	}
}

func TestSizeString(t *testing.T) {
	assert.Equal(t, "5GB", Size(5<<30).String())
	assert.Equal(t, "1536B", Size(1536).String())
}
