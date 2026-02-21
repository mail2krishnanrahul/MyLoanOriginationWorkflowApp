package scim

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type filterTokenType int

const (
	filterTokenEOF filterTokenType = iota
	filterTokenIdentifier
	filterTokenString
	filterTokenNumber
	filterTokenBool
	filterTokenLParen
	filterTokenRParen
)

type filterToken struct {
	typ filterTokenType
	val string
}

type filterParser struct {
	tokens    []filterToken
	pos       int
	nodeCount int
}

type parsedSCIMFilter struct {
	root *SCIMFilterNode
}

func (f parsedSCIMFilter) ToSQL(resource SCIMResourceType) (clause string, args []interface{}, err error) {
	if f.root == nil {
		return "", nil, nil
	}
	args = make([]interface{}, 0, 8)
	clause, err = f.root.toSQL(resource, &args)
	if err != nil {
		return "", nil, err
	}
	return clause, args, nil
}

// ParseSCIMFilter parses SCIM filter expression into an AST.
func ParseSCIMFilter(expression string) (SCIMFilter, error) {
	expression = strings.TrimSpace(expression)
	if expression == "" {
		return nil, fmt.Errorf("ParseSCIMFilter: %w", ErrInvalidSCIMFilter)
	}
	tokens, err := tokenizeFilter(expression)
	if err != nil {
		return nil, fmt.Errorf("ParseSCIMFilter: %w", err)
	}
	p := &filterParser{tokens: tokens}
	root, err := p.parseExpression()
	if err != nil {
		return nil, fmt.Errorf("ParseSCIMFilter: %w", err)
	}
	if p.peek().typ != filterTokenEOF {
		return nil, fmt.Errorf("ParseSCIMFilter: %w", ErrInvalidSCIMFilter)
	}
	if p.nodeCount > 50 {
		return nil, fmt.Errorf("ParseSCIMFilter: %w: tooMany", ErrInvalidSCIMFilter)
	}
	return parsedSCIMFilter{root: root}, nil
}

func tokenizeFilter(input string) ([]filterToken, error) {
	tokens := make([]filterToken, 0, 16)
	runes := []rune(input)
	for i := 0; i < len(runes); {
		r := runes[i]
		if unicode.IsSpace(r) {
			i++
			continue
		}
		switch r {
		case '(':
			tokens = append(tokens, filterToken{typ: filterTokenLParen, val: "("})
			i++
			continue
		case ')':
			tokens = append(tokens, filterToken{typ: filterTokenRParen, val: ")"})
			i++
			continue
		case '"':
			j := i + 1
			builder := strings.Builder{}
			escaping := false
			for ; j < len(runes); j++ {
				ch := runes[j]
				if escaping {
					switch ch {
					case '"', '\\', '/':
						builder.WriteRune(ch)
					case 'b':
						builder.WriteByte('\b')
					case 'f':
						builder.WriteByte('\f')
					case 'n':
						builder.WriteByte('\n')
					case 'r':
						builder.WriteByte('\r')
					case 't':
						builder.WriteByte('\t')
					default:
						return nil, fmt.Errorf("%w", ErrInvalidSCIMFilter)
					}
					escaping = false
					continue
				}
				if ch == '\\' {
					escaping = true
					continue
				}
				if ch == '"' {
					break
				}
				builder.WriteRune(ch)
			}
			if j >= len(runes) || runes[j] != '"' {
				return nil, fmt.Errorf("%w", ErrInvalidSCIMFilter)
			}
			tokens = append(tokens, filterToken{typ: filterTokenString, val: builder.String()})
			i = j + 1
			continue
		}

		if unicode.IsDigit(r) || r == '-' {
			j := i + 1
			for ; j < len(runes); j++ {
				if unicode.IsDigit(runes[j]) || runes[j] == '.' {
					continue
				}
				break
			}
			lit := string(runes[i:j])
			if _, err := strconv.ParseFloat(lit, 64); err == nil {
				tokens = append(tokens, filterToken{typ: filterTokenNumber, val: lit})
				i = j
				continue
			}
		}

		if isIdentifierRune(r) {
			j := i + 1
			for ; j < len(runes); j++ {
				if !isIdentifierRune(runes[j]) {
					break
				}
			}
			lit := string(runes[i:j])
			lower := strings.ToLower(strings.TrimSpace(lit))
			if lower == "true" || lower == "false" {
				tokens = append(tokens, filterToken{typ: filterTokenBool, val: lower})
			} else {
				tokens = append(tokens, filterToken{typ: filterTokenIdentifier, val: lit})
			}
			i = j
			continue
		}

		return nil, fmt.Errorf("%w", ErrInvalidSCIMFilter)
	}
	tokens = append(tokens, filterToken{typ: filterTokenEOF})
	return tokens, nil
}

func isIdentifierRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '.' || r == '-' || r == ':'
}

func (p *filterParser) peek() filterToken {
	if p.pos >= len(p.tokens) {
		return filterToken{typ: filterTokenEOF}
	}
	return p.tokens[p.pos]
}

func (p *filterParser) consume() filterToken {
	tok := p.peek()
	if p.pos < len(p.tokens) {
		p.pos++
	}
	return tok
}

func (p *filterParser) parseExpression() (*SCIMFilterNode, error) {
	return p.parseOr()
}

func (p *filterParser) parseOr() (*SCIMFilterNode, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for tok := p.peek(); tok.typ == filterTokenIdentifier && strings.EqualFold(tok.val, "or"); tok = p.peek() {
		p.consume()
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = p.newNode(&SCIMFilterNode{Kind: "logical", Operator: "or", Left: left, Right: right})
	}
	return left, nil
}

func (p *filterParser) parseAnd() (*SCIMFilterNode, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for tok := p.peek(); tok.typ == filterTokenIdentifier && strings.EqualFold(tok.val, "and"); tok = p.peek() {
		p.consume()
		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		left = p.newNode(&SCIMFilterNode{Kind: "logical", Operator: "and", Left: left, Right: right})
	}
	return left, nil
}

func (p *filterParser) parseUnary() (*SCIMFilterNode, error) {
	tok := p.peek()
	if tok.typ == filterTokenIdentifier && strings.EqualFold(tok.val, "not") {
		p.consume()
		node, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return p.newNode(&SCIMFilterNode{Kind: "not", Operator: "not", Left: node}), nil
	}
	return p.parsePrimary()
}

func (p *filterParser) parsePrimary() (*SCIMFilterNode, error) {
	tok := p.peek()
	if tok.typ == filterTokenLParen {
		p.consume()
		node, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		if p.peek().typ != filterTokenRParen {
			return nil, fmt.Errorf("%w", ErrInvalidSCIMFilter)
		}
		p.consume()
		return node, nil
	}
	return p.parseComparison()
}

func (p *filterParser) parseComparison() (*SCIMFilterNode, error) {
	attrTok := p.consume()
	if attrTok.typ != filterTokenIdentifier {
		return nil, fmt.Errorf("%w", ErrInvalidSCIMFilter)
	}
	attr := strings.ToLower(strings.TrimSpace(attrTok.val))
	if attr == "" {
		return nil, fmt.Errorf("%w", ErrInvalidSCIMFilter)
	}

	opTok := p.consume()
	if opTok.typ != filterTokenIdentifier {
		return nil, fmt.Errorf("%w", ErrInvalidSCIMFilter)
	}
	op := strings.ToLower(strings.TrimSpace(opTok.val))
	switch op {
	case "eq", "ne", "co", "sw", "ew", "pr", "gt", "ge", "lt", "le":
	default:
		return nil, fmt.Errorf("%w", ErrInvalidSCIMFilter)
	}

	node := &SCIMFilterNode{Kind: "comparison", Attribute: attr, Operator: op}
	if op == "pr" {
		return p.newNode(node), nil
	}

	valTok := p.consume()
	switch valTok.typ {
	case filterTokenString:
		node.Value = valTok.val
	case filterTokenBool:
		node.Value = strings.EqualFold(valTok.val, "true")
	case filterTokenNumber:
		n, err := strconv.ParseFloat(valTok.val, 64)
		if err != nil {
			return nil, fmt.Errorf("%w", ErrInvalidSCIMFilter)
		}
		node.Value = n
	case filterTokenIdentifier:
		node.Value = valTok.val
	default:
		return nil, fmt.Errorf("%w", ErrInvalidSCIMFilter)
	}
	return p.newNode(node), nil
}

func (p *filterParser) newNode(node *SCIMFilterNode) *SCIMFilterNode {
	p.nodeCount++
	return node
}

func (n *SCIMFilterNode) toSQL(resource SCIMResourceType, args *[]interface{}) (string, error) {
	if n == nil {
		return "", nil
	}
	switch n.Kind {
	case "logical":
		left, err := n.Left.toSQL(resource, args)
		if err != nil {
			return "", err
		}
		right, err := n.Right.toSQL(resource, args)
		if err != nil {
			return "", err
		}
		op := strings.ToUpper(n.Operator)
		if op != "AND" && op != "OR" {
			return "", fmt.Errorf("%w", ErrInvalidSCIMFilter)
		}
		return fmt.Sprintf("(%s %s %s)", left, op, right), nil
	case "not":
		inner, err := n.Left.toSQL(resource, args)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("(NOT (%s))", inner), nil
	case "comparison":
		return n.comparisonSQL(resource, args)
	default:
		return "", fmt.Errorf("%w", ErrInvalidSCIMFilter)
	}
}

func (n *SCIMFilterNode) comparisonSQL(resource SCIMResourceType, args *[]interface{}) (string, error) {
	attr := strings.ToLower(strings.TrimSpace(n.Attribute))
	op := strings.ToLower(strings.TrimSpace(n.Operator))
	def, ok := scimFilterAttribute(resource, attr)
	if !ok {
		return "", fmt.Errorf("%w: unsupported attribute %s", ErrInvalidSCIMFilter, attr)
	}

	if def.kind == "members" {
		return buildMembersClause(op, n.Value, args)
	}
	if def.kind == "active" {
		return buildActiveClause(op, n.Value, args)
	}
	if def.kind == "time" {
		return buildTimeClause(def.expr, op, n.Value, args)
	}
	return buildStringOrNumericClause(def.expr, op, n.Value, def.lower, args)
}

type filterAttrDef struct {
	expr  string
	kind  string
	lower bool
}

func scimFilterAttribute(resource SCIMResourceType, attr string) (filterAttrDef, bool) {
	attr = strings.ToLower(strings.TrimSpace(attr))
	switch resource {
	case SCIMResourceTypeUser:
		switch attr {
		case "username":
			return filterAttrDef{expr: "LOWER(u.username)", kind: "string", lower: true}, true
		case "displayname":
			return filterAttrDef{expr: "u.display_name", kind: "string", lower: false}, true
		case "emails.value":
			return filterAttrDef{expr: "LOWER(u.email)", kind: "string", lower: true}, true
		case "active":
			return filterAttrDef{kind: "active"}, true
		case "externalid":
			return filterAttrDef{expr: "u.external_id", kind: "string", lower: false}, true
		case "meta.created":
			return filterAttrDef{expr: "u.created_at", kind: "time"}, true
		case "meta.lastmodified":
			return filterAttrDef{expr: "u.updated_at", kind: "time"}, true
		}
	case SCIMResourceTypeGroup:
		switch attr {
		case "displayname":
			return filterAttrDef{expr: "g.display_name", kind: "string", lower: false}, true
		case "externalid":
			return filterAttrDef{expr: "g.external_id", kind: "string", lower: false}, true
		case "members.value":
			return filterAttrDef{kind: "members"}, true
		case "meta.created":
			return filterAttrDef{expr: "g.created_at", kind: "time"}, true
		case "meta.lastmodified":
			return filterAttrDef{expr: "g.updated_at", kind: "time"}, true
		}
	}
	return filterAttrDef{}, false
}

func nextArg(args *[]interface{}, v interface{}) string {
	*args = append(*args, v)
	return fmt.Sprintf("$%d", len(*args))
}

func buildActiveClause(op string, value interface{}, args *[]interface{}) (string, error) {
	switch op {
	case "pr":
		return "(u.status IS NOT NULL)", nil
	case "eq", "ne":
		boolValue, ok := value.(bool)
		if !ok {
			return "", fmt.Errorf("%w", ErrInvalidSCIMFilter)
		}
		statusExpr := "(u.status = 'ACTIVE')"
		if op == "eq" {
			if boolValue {
				return statusExpr, nil
			}
			return "(u.status <> 'ACTIVE')", nil
		}
		if boolValue {
			return "(u.status <> 'ACTIVE')", nil
		}
		return statusExpr, nil
	default:
		return "", fmt.Errorf("%w", ErrInvalidSCIMFilter)
	}
}

func buildMembersClause(op string, value interface{}, args *[]interface{}) (string, error) {
	base := "SELECT 1 FROM team_members tmf WHERE tmf.tenant_id = g.tenant_id AND tmf.team_id = g.team_id"
	switch op {
	case "pr":
		return fmt.Sprintf("EXISTS (%s)", base), nil
	case "eq", "ne", "co", "sw", "ew", "gt", "ge", "lt", "le":
		raw := anyToString(value)
		if raw == "" {
			return "", fmt.Errorf("%w", ErrInvalidSCIMFilter)
		}
		column := "tmf.user_id::text"
		predicateOp := op
		if op == "ne" {
			predicateOp = "eq"
		}
		predicate, err := buildSimplePredicate(column, predicateOp, raw, false, args)
		if err != nil {
			return "", err
		}
		existsExpr := fmt.Sprintf("EXISTS (%s AND %s)", base, predicate)
		if op == "ne" {
			return fmt.Sprintf("NOT (%s)", existsExpr), nil
		}
		return existsExpr, nil
	default:
		return "", fmt.Errorf("%w", ErrInvalidSCIMFilter)
	}
}

func buildTimeClause(expr string, op string, value interface{}, args *[]interface{}) (string, error) {
	if op == "pr" {
		return fmt.Sprintf("(%s IS NOT NULL)", expr), nil
	}
	raw := anyToString(value)
	if raw == "" {
		return "", fmt.Errorf("%w", ErrInvalidSCIMFilter)
	}
	if _, err := time.Parse(time.RFC3339, raw); err != nil {
		return "", fmt.Errorf("%w", ErrInvalidSCIMFilter)
	}
	placeholder := nextArg(args, raw)
	sqlOp, ok := comparisonOperator(op)
	if !ok {
		return "", fmt.Errorf("%w", ErrInvalidSCIMFilter)
	}
	return fmt.Sprintf("(%s %s %s::timestamptz)", expr, sqlOp, placeholder), nil
}

func buildStringOrNumericClause(expr string, op string, value interface{}, lower bool, args *[]interface{}) (string, error) {
	if op == "pr" {
		return fmt.Sprintf("(%s IS NOT NULL)", expr), nil
	}
	raw := value
	if lower {
		if s, ok := value.(string); ok {
			raw = strings.ToLower(s)
		}
	}
	strValue := anyToString(raw)
	if strValue == "" && op != "eq" && op != "ne" {
		return "", fmt.Errorf("%w", ErrInvalidSCIMFilter)
	}
	return buildSimplePredicate(expr, op, strValue, lower, args)
}

func buildSimplePredicate(expr string, op string, value string, lower bool, args *[]interface{}) (string, error) {
	switch op {
	case "eq", "ne", "gt", "ge", "lt", "le":
		sqlOp, ok := comparisonOperator(op)
		if !ok {
			return "", fmt.Errorf("%w", ErrInvalidSCIMFilter)
		}
		placeholder := nextArg(args, value)
		if lower {
			return fmt.Sprintf("(%s %s LOWER(%s))", expr, sqlOp, placeholder), nil
		}
		return fmt.Sprintf("(%s %s %s)", expr, sqlOp, placeholder), nil
	case "co":
		placeholder := nextArg(args, "%"+value+"%")
		return fmt.Sprintf("(%s ILIKE %s)", expr, placeholder), nil
	case "sw":
		placeholder := nextArg(args, value+"%")
		return fmt.Sprintf("(%s ILIKE %s)", expr, placeholder), nil
	case "ew":
		placeholder := nextArg(args, "%"+value)
		return fmt.Sprintf("(%s ILIKE %s)", expr, placeholder), nil
	default:
		return "", fmt.Errorf("%w", ErrInvalidSCIMFilter)
	}
}

func comparisonOperator(op string) (string, bool) {
	switch strings.ToLower(op) {
	case "eq":
		return "=", true
	case "ne":
		return "<>", true
	case "gt":
		return ">", true
	case "ge":
		return ">=", true
	case "lt":
		return "<", true
	case "le":
		return "<=", true
	default:
		return "", false
	}
}
