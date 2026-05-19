package config

import (
	"math"
	"testing"
	"time"

	"github.com/spf13/viper"
)

func TestScaleBareNumber(t *testing.T) {
	t.Parallel()

	fallback := 5 * time.Second
	unit := time.Millisecond

	tests := []struct {
		name string
		n    float64
		want time.Duration
	}{
		{name: "normal int", n: 200, want: 200 * time.Millisecond},
		{name: "normal float", n: 1.5, want: time.Duration(1.5 * float64(time.Millisecond))},
		{name: "zero", n: 0, want: fallback},
		{name: "negative", n: -1, want: fallback},
		{name: "NaN", n: math.NaN(), want: fallback},
		{name: "positive Inf", n: math.Inf(1), want: fallback},
		{name: "overflow int64", n: float64(math.MaxInt64), want: fallback},
		{name: "huge float64", n: 1e20, want: fallback},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := scaleBareNumber(tc.n, unit, fallback); got != tc.want {
				t.Fatalf("scaleBareNumber(%v) = %v, want %v", tc.n, got, tc.want)
			}
		})
	}
}

func TestScaleBareNumber_AtLimit(t *testing.T) {
	t.Parallel()

	fallback := time.Second
	unit := time.Millisecond
	maxBare := float64(math.MaxInt64) / float64(unit)
	n := maxBare - 1

	got := scaleBareNumber(n, unit, fallback)
	want := time.Duration(n * float64(unit))
	if got != want {
		t.Fatalf("scaleBareNumber(at limit) = %v, want %v", got, want)
	}
}

func TestParseViperDuration(t *testing.T) {
	t.Parallel()

	fallback := 200 * time.Millisecond
	unit := time.Millisecond

	tests := []struct {
		name    string
		set     func(*viper.Viper)
		want    time.Duration
		wantSet bool
	}{
		{
			name: "bare int",
			set: func(v *viper.Viper) {
				v.Set("delay", 500)
			},
			want:    500 * time.Millisecond,
			wantSet: true,
		},
		{
			name: "duration string",
			set: func(v *viper.Viper) {
				v.Set("delay", "300ms")
			},
			want:    300 * time.Millisecond,
			wantSet: true,
		},
		{
			name:    "unset uses fallback",
			set:     func(*viper.Viper) {},
			want:    fallback,
			wantSet: false,
		},
		{
			name: "overflow bare int",
			set: func(v *viper.Viper) {
				v.Set("delay", int64(math.MaxInt64))
			},
			want:    fallback,
			wantSet: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			v := viper.New()
			tc.set(v)
			got := parseViperDuration(v, "delay", fallback, unit)
			if got != tc.want {
				t.Fatalf("parseViperDuration() = %v, want %v", got, tc.want)
			}
			if tc.wantSet && !v.IsSet("delay") {
				t.Fatal("expected key to be set")
			}
		})
	}
}
