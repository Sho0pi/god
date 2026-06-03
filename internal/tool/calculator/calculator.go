package calculator

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"

	"github.com/sho0pi/god/internal/tool"
)

// Tool evaluates math expressions with no external dependencies.
type Tool struct{}

func New() *Tool { return &Tool{} }

func (t *Tool) Name() string { return "calculator" }

func (t *Tool) Description() string {
	return "Evaluate a math expression. Supports +, -, *, /, %, ^ (power), " +
		"parentheses, and functions: sqrt, abs, floor, ceil, round, sin, cos, tan, log, log2, exp."
}

func (t *Tool) Schema() *tool.Schema {
	return &tool.Schema{
		Properties: map[string]*tool.Property{
			"expression": {
				Type:        "string",
				Description: "Math expression to evaluate, e.g. '2 + 3 * (4 - 1)' or 'sqrt(144) / 2'",
			},
		},
		Required: []string{"expression"},
	}
}

func (t *Tool) Execute(_ context.Context, args map[string]any) (string, error) {
	expr, _ := args["expression"].(string)
	if expr == "" {
		return "", fmt.Errorf("expression is required")
	}
	result, err := evaluate(expr)
	if err != nil {
		return "", fmt.Errorf("invalid expression: %w", err)
	}
	if result == math.Trunc(result) {
		return fmt.Sprintf("%g", result), nil
	}
	return strconv.FormatFloat(result, 'f', -1, 64), nil
}

// --- recursive descent parser ---
// Grammar:
//   expr   = term (('+' | '-') term)*
//   term   = factor (('*' | '/' | '%') factor)*
//   factor = unary ('^' unary)*
//   unary  = '-' unary | primary
//   primary = number | ident '(' expr ')' | '(' expr ')'

type parser struct {
	src string
	pos int
}

func evaluate(expr string) (float64, error) {
	p := &parser{src: strings.TrimSpace(expr)}
	val, err := p.parseExpr()
	if err != nil {
		return 0, err
	}
	p.skipSpace()
	if p.pos < len(p.src) {
		return 0, fmt.Errorf("unexpected character %q at position %d", p.src[p.pos], p.pos)
	}
	return val, nil
}

func (p *parser) skipSpace() {
	for p.pos < len(p.src) && unicode.IsSpace(rune(p.src[p.pos])) {
		p.pos++
	}
}

func (p *parser) parseExpr() (float64, error) {
	left, err := p.parseTerm()
	if err != nil {
		return 0, err
	}
	for {
		p.skipSpace()
		if p.pos >= len(p.src) {
			break
		}
		op := p.src[p.pos]
		if op != '+' && op != '-' {
			break
		}
		p.pos++
		right, err := p.parseTerm()
		if err != nil {
			return 0, err
		}
		if op == '+' {
			left += right
		} else {
			left -= right
		}
	}
	return left, nil
}

func (p *parser) parseTerm() (float64, error) {
	left, err := p.parseFactor()
	if err != nil {
		return 0, err
	}
	for {
		p.skipSpace()
		if p.pos >= len(p.src) {
			break
		}
		op := p.src[p.pos]
		if op != '*' && op != '/' && op != '%' {
			break
		}
		p.pos++
		right, err := p.parseFactor()
		if err != nil {
			return 0, err
		}
		switch op {
		case '*':
			left *= right
		case '/':
			if right == 0 {
				return 0, fmt.Errorf("division by zero")
			}
			left /= right
		case '%':
			if right == 0 {
				return 0, fmt.Errorf("modulo by zero")
			}
			left = math.Mod(left, right)
		}
	}
	return left, nil
}

func (p *parser) parseFactor() (float64, error) {
	base, err := p.parseUnary()
	if err != nil {
		return 0, err
	}
	p.skipSpace()
	if p.pos < len(p.src) && p.src[p.pos] == '^' {
		p.pos++
		exp, err := p.parseUnary()
		if err != nil {
			return 0, err
		}
		base = math.Pow(base, exp)
	}
	return base, nil
}

func (p *parser) parseUnary() (float64, error) {
	p.skipSpace()
	if p.pos < len(p.src) && p.src[p.pos] == '-' {
		p.pos++
		val, err := p.parseUnary()
		return -val, err
	}
	return p.parsePrimary()
}

func (p *parser) parsePrimary() (float64, error) {
	p.skipSpace()
	if p.pos >= len(p.src) {
		return 0, fmt.Errorf("unexpected end of expression")
	}

	// parenthesised expression
	if p.src[p.pos] == '(' {
		p.pos++
		val, err := p.parseExpr()
		if err != nil {
			return 0, err
		}
		p.skipSpace()
		if p.pos >= len(p.src) || p.src[p.pos] != ')' {
			return 0, fmt.Errorf("missing closing parenthesis")
		}
		p.pos++
		return val, nil
	}

	// function call or named constant
	if unicode.IsLetter(rune(p.src[p.pos])) {
		start := p.pos
		for p.pos < len(p.src) && (unicode.IsLetter(rune(p.src[p.pos])) || unicode.IsDigit(rune(p.src[p.pos]))) {
			p.pos++
		}
		name := strings.ToLower(p.src[start:p.pos])

		// named constants
		switch name {
		case "pi":
			return math.Pi, nil
		case "e":
			return math.E, nil
		}

		// function call
		p.skipSpace()
		if p.pos >= len(p.src) || p.src[p.pos] != '(' {
			return 0, fmt.Errorf("unknown identifier %q", name)
		}
		p.pos++ // consume '('
		arg, err := p.parseExpr()
		if err != nil {
			return 0, err
		}
		p.skipSpace()
		if p.pos >= len(p.src) || p.src[p.pos] != ')' {
			return 0, fmt.Errorf("missing ')' after function %q", name)
		}
		p.pos++ // consume ')'
		return applyFunc(name, arg)
	}

	// number
	start := p.pos
	if p.pos < len(p.src) && p.src[p.pos] == '+' {
		p.pos++
	}
	for p.pos < len(p.src) && (unicode.IsDigit(rune(p.src[p.pos])) || p.src[p.pos] == '.') {
		p.pos++
	}
	if p.pos < len(p.src) && (p.src[p.pos] == 'e' || p.src[p.pos] == 'E') {
		p.pos++
		if p.pos < len(p.src) && (p.src[p.pos] == '+' || p.src[p.pos] == '-') {
			p.pos++
		}
		for p.pos < len(p.src) && unicode.IsDigit(rune(p.src[p.pos])) {
			p.pos++
		}
	}
	raw := p.src[start:p.pos]
	if raw == "" {
		return 0, fmt.Errorf("unexpected character %q", p.src[p.pos])
	}
	return strconv.ParseFloat(raw, 64)
}

func applyFunc(name string, arg float64) (float64, error) {
	switch name {
	case "sqrt":
		if arg < 0 {
			return 0, fmt.Errorf("sqrt of negative number")
		}
		return math.Sqrt(arg), nil
	case "abs":
		return math.Abs(arg), nil
	case "floor":
		return math.Floor(arg), nil
	case "ceil":
		return math.Ceil(arg), nil
	case "round":
		return math.Round(arg), nil
	case "sin":
		return math.Sin(arg), nil
	case "cos":
		return math.Cos(arg), nil
	case "tan":
		return math.Tan(arg), nil
	case "log", "ln":
		if arg <= 0 {
			return 0, fmt.Errorf("log of non-positive number")
		}
		return math.Log(arg), nil
	case "log2":
		if arg <= 0 {
			return 0, fmt.Errorf("log2 of non-positive number")
		}
		return math.Log2(arg), nil
	case "log10":
		if arg <= 0 {
			return 0, fmt.Errorf("log10 of non-positive number")
		}
		return math.Log10(arg), nil
	case "exp":
		return math.Exp(arg), nil
	default:
		return 0, fmt.Errorf("unknown function %q", name)
	}
}
