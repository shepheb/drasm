package core

// Operator is the type for Expression operators.
// It's machine-independent, and each parser is responsible for generating the
// right Operator from its parsed strings or tokens.
type Operator int

// Operator values
const (
	PLUS Operator = iota + 300
	MINUS
	TIMES
	DIVIDE
	LANGLES
	RANGLES
	AND
	OR
	XOR
	NOT
	ILLEGAL
)

// OperatorNames maps operator name strings to the internal values.
var OperatorNames map[string]Operator = map[string]Operator{
	"+":  PLUS,
	"-":  MINUS,
	"*":  TIMES,
	"/":  DIVIDE,
	"<<": LANGLES,
	">>": RANGLES,
	"&":  AND,
	"|":  OR,
	"^":  XOR,
	"~":  NOT,
}
