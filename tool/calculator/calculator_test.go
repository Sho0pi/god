package calculator_test

import (
	"context"
	"math"
	"strconv"
	"testing"

	"github.com/sho0pi/god/tool/calculator"
)

func TestCalculator(t *testing.T) {
	tool := calculator.New()
	ctx := context.Background()

	exec := func(expr string) (float64, error) {
		t.Helper()
		result, err := tool.Execute(ctx, map[string]any{"expression": expr})
		if err != nil {
			return 0, err
		}
		return strconv.ParseFloat(result, 64)
	}

	cases := []struct {
		expr string
		want float64
	}{
		{"2 + 3", 5},
		{"10 - 4", 6},
		{"3 * 4", 12},
		{"10 / 4", 2.5},
		{"10 % 3", 1},
		{"2 ^ 10", 1024},
		{"(2 + 3) * 4", 20},
		{"-5 + 3", -2},
		{"sqrt(144)", 12},
		{"abs(-7)", 7},
		{"floor(3.9)", 3},
		{"ceil(3.1)", 4},
		{"round(3.5)", 4},
		{"2 + 3 * 4", 14}, // operator precedence
		{"pi", math.Pi},
		{"e", math.E},
		{"log(1)", 0},
		{"exp(0)", 1},
	}

	for _, tc := range cases {
		got, err := exec(tc.expr)
		if err != nil {
			t.Errorf("%q: unexpected error: %v", tc.expr, err)
			continue
		}
		if math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("%q = %v, want %v", tc.expr, got, tc.want)
		}
	}
}

func TestCalculator_Errors(t *testing.T) {
	tool := calculator.New()
	ctx := context.Background()

	errCases := []string{
		"1 / 0",
		"sqrt(-1)",
		"log(-1)",
		"unknown(5)",
		"2 +",
		"",
	}

	for _, expr := range errCases {
		_, err := tool.Execute(ctx, map[string]any{"expression": expr})
		if err == nil {
			t.Errorf("%q: expected error, got nil", expr)
		}
	}
}
