package calc

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"
)

// 表达式求值器（递归下降），不引入任何第三方依赖。
//
// 为什么需要它：大语言模型做多位数算术并不可靠，它是在"续写"数字而不是在计算。
// 把算术交给代码执行，结果才可信。
//
// 支持：+ - * / % ^、括号、一元负号、常量 pi/e，
// 以及 sqrt abs min max floor ceil round pow log ln sin cos tan 等函数。

type parser struct {
	input []rune
	pos   int
}

// Eval 求值一个数学表达式
func Eval(expr string) (float64, error) {
	if strings.TrimSpace(expr) == "" {
		return 0, fmt.Errorf("表达式为空")
	}
	p := &parser{input: []rune(expr)}
	v, err := p.parseExpr()
	if err != nil {
		return 0, err
	}
	p.skipSpaces()
	if p.pos < len(p.input) {
		return 0, fmt.Errorf("表达式中存在无法解析的内容: %q", string(p.input[p.pos:]))
	}
	if math.IsNaN(v) {
		return 0, fmt.Errorf("计算结果无意义（NaN）")
	}
	if math.IsInf(v, 0) {
		return 0, fmt.Errorf("计算结果溢出")
	}
	return v, nil
}

// Format 把结果格式化成便于阅读的字符串
func Format(v float64) string {
	// 接近整数时按整数输出，避免 6 显示成 6.000000
	if v == math.Trunc(v) && math.Abs(v) < 1e15 {
		return strconv.FormatFloat(v, 'f', 0, 64)
	}
	s := strconv.FormatFloat(v, 'f', -1, 64)
	if len(s) > 18 {
		s = strconv.FormatFloat(v, 'g', 12, 64)
	}
	return s
}

func (p *parser) skipSpaces() {
	for p.pos < len(p.input) && unicode.IsSpace(p.input[p.pos]) {
		p.pos++
	}
}

func (p *parser) peek() rune {
	p.skipSpaces()
	if p.pos < len(p.input) {
		return p.input[p.pos]
	}
	return 0
}

// parseExpr 处理加减
func (p *parser) parseExpr() (float64, error) {
	left, err := p.parseTerm()
	if err != nil {
		return 0, err
	}
	for {
		switch p.peek() {
		case '+':
			p.pos++
			r, err := p.parseTerm()
			if err != nil {
				return 0, err
			}
			left += r
		case '-':
			p.pos++
			r, err := p.parseTerm()
			if err != nil {
				return 0, err
			}
			left -= r
		default:
			return left, nil
		}
	}
}

// parseTerm 处理乘除取模
func (p *parser) parseTerm() (float64, error) {
	left, err := p.parseUnary()
	if err != nil {
		return 0, err
	}
	for {
		switch p.peek() {
		case '*':
			p.pos++
			r, err := p.parseUnary()
			if err != nil {
				return 0, err
			}
			left *= r
		case '/':
			p.pos++
			r, err := p.parseUnary()
			if err != nil {
				return 0, err
			}
			if r == 0 {
				return 0, fmt.Errorf("除数为零")
			}
			left /= r
		case '%':
			p.pos++
			r, err := p.parseUnary()
			if err != nil {
				return 0, err
			}
			if r == 0 {
				return 0, fmt.Errorf("取模的除数为零")
			}
			left = math.Mod(left, r)
		default:
			return left, nil
		}
	}
}

func (p *parser) parseUnary() (float64, error) {
	switch p.peek() {
	case '-':
		p.pos++
		v, err := p.parseUnary()
		return -v, err
	case '+':
		p.pos++
		return p.parseUnary()
	}
	return p.parsePower()
}

// parsePower 处理幂运算（右结合）
func (p *parser) parsePower() (float64, error) {
	base, err := p.parseAtom()
	if err != nil {
		return 0, err
	}
	if p.peek() == '^' {
		p.pos++
		exp, err := p.parseUnary()
		if err != nil {
			return 0, err
		}
		return math.Pow(base, exp), nil
	}
	return base, nil
}

func (p *parser) parseAtom() (float64, error) {
	p.skipSpaces()
	if p.pos >= len(p.input) {
		return 0, fmt.Errorf("表达式意外结束")
	}

	c := p.input[p.pos]

	if c == '(' {
		p.pos++
		v, err := p.parseExpr()
		if err != nil {
			return 0, err
		}
		if p.peek() != ')' {
			return 0, fmt.Errorf("缺少右括号")
		}
		p.pos++
		return v, nil
	}

	if unicode.IsDigit(c) || c == '.' {
		return p.parseNumber()
	}

	if unicode.IsLetter(c) {
		return p.parseIdent()
	}

	return 0, fmt.Errorf("无法识别的字符: %q", string(c))
}

func (p *parser) parseNumber() (float64, error) {
	start := p.pos
	for p.pos < len(p.input) {
		c := p.input[p.pos]
		if unicode.IsDigit(c) || c == '.' {
			p.pos++
			continue
		}
		// 科学计数法
		if (c == 'e' || c == 'E') && p.pos+1 < len(p.input) {
			next := p.input[p.pos+1]
			if unicode.IsDigit(next) || next == '+' || next == '-' {
				p.pos += 2
				continue
			}
		}
		break
	}
	text := string(p.input[start:p.pos])
	v, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return 0, fmt.Errorf("无法解析数字: %q", text)
	}
	return v, nil
}

func (p *parser) parseIdent() (float64, error) {
	start := p.pos
	for p.pos < len(p.input) && (unicode.IsLetter(p.input[p.pos]) || unicode.IsDigit(p.input[p.pos]) || p.input[p.pos] == '_') {
		p.pos++
	}
	name := strings.ToLower(string(p.input[start:p.pos]))

	// 常量
	switch name {
	case "pi":
		return math.Pi, nil
	case "e":
		return math.E, nil
	}

	// 函数调用
	if p.peek() != '(' {
		return 0, fmt.Errorf("未知的标识符: %q", name)
	}
	p.pos++

	args := []float64{}
	if p.peek() != ')' {
		for {
			v, err := p.parseExpr()
			if err != nil {
				return 0, err
			}
			args = append(args, v)
			if p.peek() == ',' {
				p.pos++
				continue
			}
			break
		}
	}
	if p.peek() != ')' {
		return 0, fmt.Errorf("函数 %s 缺少右括号", name)
	}
	p.pos++

	return applyFunc(name, args)
}

func applyFunc(name string, args []float64) (float64, error) {
	need := func(n int) error {
		if len(args) != n {
			return fmt.Errorf("函数 %s 需要 %d 个参数，收到 %d 个", name, n, len(args))
		}
		return nil
	}

	switch name {
	case "sqrt":
		if err := need(1); err != nil {
			return 0, err
		}
		if args[0] < 0 {
			return 0, fmt.Errorf("sqrt 的参数不能为负数")
		}
		return math.Sqrt(args[0]), nil
	case "abs":
		if err := need(1); err != nil {
			return 0, err
		}
		return math.Abs(args[0]), nil
	case "floor":
		if err := need(1); err != nil {
			return 0, err
		}
		return math.Floor(args[0]), nil
	case "ceil":
		if err := need(1); err != nil {
			return 0, err
		}
		return math.Ceil(args[0]), nil
	case "round":
		if err := need(1); err != nil {
			return 0, err
		}
		return math.Round(args[0]), nil
	case "ln":
		if err := need(1); err != nil {
			return 0, err
		}
		if args[0] <= 0 {
			return 0, fmt.Errorf("ln 的参数必须为正数")
		}
		return math.Log(args[0]), nil
	case "log":
		if err := need(1); err != nil {
			return 0, err
		}
		if args[0] <= 0 {
			return 0, fmt.Errorf("log 的参数必须为正数")
		}
		return math.Log10(args[0]), nil
	case "sin":
		if err := need(1); err != nil {
			return 0, err
		}
		return math.Sin(args[0]), nil
	case "cos":
		if err := need(1); err != nil {
			return 0, err
		}
		return math.Cos(args[0]), nil
	case "tan":
		if err := need(1); err != nil {
			return 0, err
		}
		return math.Tan(args[0]), nil
	case "pow":
		if err := need(2); err != nil {
			return 0, err
		}
		return math.Pow(args[0], args[1]), nil
	case "min":
		if len(args) < 2 {
			return 0, fmt.Errorf("min 至少需要 2 个参数")
		}
		m := args[0]
		for _, v := range args[1:] {
			m = math.Min(m, v)
		}
		return m, nil
	case "max":
		if len(args) < 2 {
			return 0, fmt.Errorf("max 至少需要 2 个参数")
		}
		m := args[0]
		for _, v := range args[1:] {
			m = math.Max(m, v)
		}
		return m, nil
	}

	return 0, fmt.Errorf("不支持的函数: %s", name)
}
