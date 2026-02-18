package approval

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// ExpressionEvaluator evaluates simple boolean expressions against a JSON-like map.
type ExpressionEvaluator struct{}

// Evaluate parses and evaluates an expression.
// Supported operators: ==, !=, <, >, <=, >=, &&, || and dot notation lookups.
func (e *ExpressionEvaluator) Evaluate(
	ctx context.Context,
	expression string,
	contextData map[string]interface{},
) (bool, error) {
	_ = ctx

	trimmed := strings.TrimSpace(expression)
	if trimmed == "" {
		return false, fmt.Errorf("Evaluate: %w: expression is empty", ErrInvalidExpression)
	}
	if contextData == nil {
		contextData = map[string]interface{}{}
	}

	tokens, err := tokenize(trimmed)
	if err != nil {
		return false, fmt.Errorf("Evaluate: %w", err)
	}

	p := &exprParser{tokens: tokens, ctx: contextData}
	value, err := p.parseExpression()
	if err != nil {
		return false, fmt.Errorf("Evaluate: %w", err)
	}
	if p.current().typ != tokenEOF {
		return false, fmt.Errorf("Evaluate: %w: unexpected token %q", ErrInvalidExpression, p.current().text)
	}

	asBool, ok := asBool(value)
	if !ok {
		return false, fmt.Errorf("Evaluate: %w: final expression is not boolean", ErrExpressionTypeMismatch)
	}

	return asBool, nil
}

type tokenType int

const (
	tokenEOF tokenType = iota
	tokenIdentifier
	tokenNumber
	tokenString
	tokenBool
	tokenLParen
	tokenRParen
	tokenAnd
	tokenOr
	tokenEq
	tokenNe
	tokenLt
	tokenGt
	tokenLe
	tokenGe
)

type token struct {
	typ tokenType
	text string
	pos int
}

func tokenize(expr string) ([]token, error) {
	out := make([]token, 0, len(expr)/2)
	for i := 0; i < len(expr); {
		ch := expr[i]
		switch {
		case ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r':
			i++
		case ch == '(':
			out = append(out, token{typ: tokenLParen, text: "(", pos: i})
			i++
		case ch == ')':
			out = append(out, token{typ: tokenRParen, text: ")", pos: i})
			i++
		case i+1 < len(expr) && expr[i:i+2] == "&&":
			out = append(out, token{typ: tokenAnd, text: "&&", pos: i})
			i += 2
		case i+1 < len(expr) && expr[i:i+2] == "||":
			out = append(out, token{typ: tokenOr, text: "||", pos: i})
			i += 2
		case i+1 < len(expr) && expr[i:i+2] == "==":
			out = append(out, token{typ: tokenEq, text: "==", pos: i})
			i += 2
		case i+1 < len(expr) && expr[i:i+2] == "!=":
			out = append(out, token{typ: tokenNe, text: "!=", pos: i})
			i += 2
		case i+1 < len(expr) && expr[i:i+2] == "<=":
			out = append(out, token{typ: tokenLe, text: "<=", pos: i})
			i += 2
		case i+1 < len(expr) && expr[i:i+2] == ">=":
			out = append(out, token{typ: tokenGe, text: ">=", pos: i})
			i += 2
		case ch == '<':
			out = append(out, token{typ: tokenLt, text: "<", pos: i})
			i++
		case ch == '>':
			out = append(out, token{typ: tokenGt, text: ">", pos: i})
			i++
		case ch == '\'' || ch == '"':
			quote := ch
			start := i
			i++
			var sb strings.Builder
			for i < len(expr) {
				if expr[i] == '\\' {
					if i+1 >= len(expr) {
						return nil, &SyntaxError{Position: start, Message: "unterminated escape sequence"}
					}
					sb.WriteByte(expr[i+1])
					i += 2
					continue
				}
				if expr[i] == quote {
					break
				}
				sb.WriteByte(expr[i])
				i++
			}
			if i >= len(expr) || expr[i] != quote {
				return nil, &SyntaxError{Position: start, Message: "unterminated string literal"}
			}
			i++
			out = append(out, token{typ: tokenString, text: sb.String(), pos: start})
		case isNumberStart(ch):
			start := i
			i++
			for i < len(expr) && (isDigit(expr[i]) || expr[i] == '.') {
				i++
			}
			out = append(out, token{typ: tokenNumber, text: expr[start:i], pos: start})
		case isIdentifierStart(ch):
			start := i
			i++
			for i < len(expr) && isIdentifierPart(expr[i]) {
				i++
			}
			word := expr[start:i]
			switch strings.ToLower(word) {
			case "true", "false":
				out = append(out, token{typ: tokenBool, text: strings.ToLower(word), pos: start})
			default:
				out = append(out, token{typ: tokenIdentifier, text: word, pos: start})
			}
		default:
			return nil, &SyntaxError{Position: i, Message: "unexpected character " + string(ch)}
		}
	}
	out = append(out, token{typ: tokenEOF, pos: len(expr)})
	return out, nil
}

func isNumberStart(ch byte) bool {
	return isDigit(ch) || ch == '-'
}

func isDigit(ch byte) bool {
	return ch >= '0' && ch <= '9'
}

func isIdentifierStart(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '_'
}

func isIdentifierPart(ch byte) bool {
	return isIdentifierStart(ch) || isDigit(ch) || ch == '.'
}

type exprParser struct {
	tokens []token
	pos    int
	ctx    map[string]interface{}
}

func (p *exprParser) current() token {
	if p.pos >= len(p.tokens) {
		return token{typ: tokenEOF}
	}
	return p.tokens[p.pos]
}

func (p *exprParser) consume() token {
	tok := p.current()
	if p.pos < len(p.tokens) {
		p.pos++
	}
	return tok
}

func (p *exprParser) parseExpression() (interface{}, error) {
	return p.parseOr()
}

func (p *exprParser) parseOr() (interface{}, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.current().typ == tokenOr {
		p.consume()
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		lb, ok := asBool(left)
		if !ok {
			return nil, &TypeError{Message: "left side of || is not boolean"}
		}
		rb, ok := asBool(right)
		if !ok {
			return nil, &TypeError{Message: "right side of || is not boolean"}
		}
		left = lb || rb
	}
	return left, nil
}

func (p *exprParser) parseAnd() (interface{}, error) {
	left, err := p.parseComparison()
	if err != nil {
		return nil, err
	}
	for p.current().typ == tokenAnd {
		p.consume()
		right, err := p.parseComparison()
		if err != nil {
			return nil, err
		}
		lb, ok := asBool(left)
		if !ok {
			return nil, &TypeError{Message: "left side of && is not boolean"}
		}
		rb, ok := asBool(right)
		if !ok {
			return nil, &TypeError{Message: "right side of && is not boolean"}
		}
		left = lb && rb
	}
	return left, nil
}

func (p *exprParser) parseComparison() (interface{}, error) {
	left, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}

	tok := p.current()
	switch tok.typ {
	case tokenEq, tokenNe, tokenLt, tokenGt, tokenLe, tokenGe:
		p.consume()
		right, err := p.parsePrimary()
		if err != nil {
			return nil, err
		}
		result, err := evaluateComparison(tok.typ, left, right)
		if err != nil {
			return nil, err
		}
		return result, nil
	default:
		return left, nil
	}
}

func (p *exprParser) parsePrimary() (interface{}, error) {
	tok := p.current()
	switch tok.typ {
	case tokenLParen:
		p.consume()
		value, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		if p.current().typ != tokenRParen {
			return nil, &SyntaxError{Position: p.current().pos, Message: "expected ')'"}
		}
		p.consume()
		return value, nil

	case tokenNumber:
		p.consume()
		n, err := strconv.ParseFloat(tok.text, 64)
		if err != nil {
			return nil, &SyntaxError{Position: tok.pos, Message: "invalid numeric literal"}
		}
		return n, nil

	case tokenString:
		p.consume()
		return tok.text, nil

	case tokenBool:
		p.consume()
		return tok.text == "true", nil

	case tokenIdentifier:
		p.consume()
		value, ok := lookupPath(p.ctx, tok.text)
		if !ok {
			return nil, &TypeError{Message: "unknown field: " + tok.text}
		}
		return value, nil

	default:
		return nil, &SyntaxError{Position: tok.pos, Message: "expected literal, identifier, or '('"}
	}
}

func lookupPath(ctx map[string]interface{}, path string) (interface{}, bool) {
	parts := strings.Split(path, ".")
	var current interface{} = ctx
	for _, part := range parts {
		obj, ok := current.(map[string]interface{})
		if !ok {
			return nil, false
		}
		value, exists := obj[part]
		if !exists {
			return nil, false
		}
		current = value
	}
	return current, true
}

func evaluateComparison(op tokenType, left, right interface{}) (bool, error) {
	if lNum, lOk := asFloat(left); lOk {
		rNum, rOk := asFloat(right)
		if !rOk {
			return false, &TypeError{Message: "numeric comparison with non-numeric right operand"}
		}
		switch op {
		case tokenEq:
			return lNum == rNum, nil
		case tokenNe:
			return lNum != rNum, nil
		case tokenLt:
			return lNum < rNum, nil
		case tokenGt:
			return lNum > rNum, nil
		case tokenLe:
			return lNum <= rNum, nil
		case tokenGe:
			return lNum >= rNum, nil
		}
	}

	if lBool, lOk := asBool(left); lOk {
		rBool, rOk := asBool(right)
		if !rOk {
			return false, &TypeError{Message: "boolean comparison with non-boolean right operand"}
		}
		switch op {
		case tokenEq:
			return lBool == rBool, nil
		case tokenNe:
			return lBool != rBool, nil
		default:
			return false, &TypeError{Message: "boolean supports only == and !="}
		}
	}

	lText := fmt.Sprint(left)
	rText := fmt.Sprint(right)
	switch op {
	case tokenEq:
		return lText == rText, nil
	case tokenNe:
		return lText != rText, nil
	case tokenLt:
		return lText < rText, nil
	case tokenGt:
		return lText > rText, nil
	case tokenLe:
		return lText <= rText, nil
	case tokenGe:
		return lText >= rText, nil
	default:
		return false, &TypeError{Message: "unsupported comparison operator"}
	}
}

func asBool(v interface{}) (bool, bool) {
	switch t := v.(type) {
	case bool:
		return t, true
	case string:
		s := strings.TrimSpace(strings.ToLower(t))
		if s == "true" {
			return true, true
		}
		if s == "false" {
			return false, true
		}
	}
	return false, false
}

func asFloat(v interface{}) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	case int32:
		return float64(t), true
	case uint64:
		return float64(t), true
	case uint32:
		return float64(t), true
	case uint:
		return float64(t), true
	case string:
		n, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
		if err == nil {
			return n, true
		}
	}
	return 0, false
}
