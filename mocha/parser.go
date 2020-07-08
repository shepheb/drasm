package mocha

import (
	"fmt"

	"github.com/shepheb/drasm/core"
	"github.com/shepheb/psec"
)

// Wrap the most common parser ops for brevity.
func lit(s string) psec.Parser {
	return psec.Literal(s)
}
func litIC(s string) psec.Parser {
	return psec.LiteralIC(s)
}
func sym(s string) psec.Parser {
	return psec.Symbol(s)
}
func ws() psec.Parser {
	return psec.Symbol("ws")
}
func seq(args ...psec.Parser) psec.Parser {
	return psec.Seq(args...)
}
func alts(names ...string) psec.Parser {
	var ps []psec.Parser
	for _, s := range names {
		ps = append(ps, litIC(s))
	}
	return psec.Alt(ps...)
}

var regNumbers = map[byte]uint16{
	'A': 0,
	'B': 1,
	'C': 2,
	'X': 3,
	'Y': 4,
	'Z': 5,
	'I': 6,
	'J': 7,
	'a': 0,
	'b': 1,
	'c': 2,
	'x': 3,
	'y': 4,
	'z': 5,
	'i': 6,
	'j': 7,
}

func buildMochaParser() *psec.Grammar {
	g := psec.NewGrammar()
	core.AddBasicParsers(g)

	g.WithAction("gpReg", psec.OneOf("ABCXYZIJabcxyzij"),
		func(r interface{}, loc *psec.Loc) (interface{}, error) {
			return regDirect(regNumbers[r.(byte)]), nil
		})

	g.WithAction("sp", litIC("sp"),
		func(r interface{}, loc *psec.Loc) (interface{}, error) {
			return &specialReg{sp: true}, nil
		})
	g.WithAction("pc", litIC("pc"),
		func(r interface{}, loc *psec.Loc) (interface{}, error) {
			return &specialReg{pc: true}, nil
		})
	g.WithAction("ex", litIC("ex"),
		func(r interface{}, loc *psec.Loc) (interface{}, error) {
			return &specialReg{ex: true}, nil
		})
	g.WithAction("ia", litIC("ia"),
		func(r interface{}, loc *psec.Loc) (interface{}, error) {
			return &specialReg{ia: true}, nil
		})

	g.AddSymbol("comma", psec.Seq(sym("wsline"), lit(","), sym("wsline")))

	// This only pretends to be optional. It errors if not provided; but the error
	// message is more detailed.
	g.WithAction("suffix", psec.Optional(psec.Alt(litIC(".l"), litIC(".w"))),
		func(r interface{}, loc *psec.Loc) (interface{}, error) {
			if size, ok := r.(string); ok {
				return size == ".l", nil
			}
			// The detailed "size suffix" error is lost because of sepBy.
			core.AsmError(loc, "size suffix required")
			return nil, fmt.Errorf("size suffix required")
		})

	addAddressingModeParsers(g)
	addBinaryOpParsers(g)
	addBitTwiddlerParsers(g)
	addUnaryOpParsers(g)
	addRegOpParsers(g)
	addNullaryOpParsers(g)
	addBranchOpParsers(g)
	addSetOpParsers(g)

	g.AddSymbol("instruction",
		psec.Alt(sym("binary instruction"), sym("unary instruction"),
			sym("reg instruction"), sym("nullary instruction"),
			sym("twiddler instruction"), sym("branch instruction"),
			sym("set instruction"), sym("lea instruction")))

	return g
}

func addAddressingModeParsers(g *psec.Grammar) {
	g.AddSymbol("operand",
		psec.Alt(sym("am push/pop"), sym("am peek"), sym("am special reg"),
			sym("am pc-rel"), sym("am sp-rel"), sym("gpReg"), sym("am reg increment"),
			sym("am reg indirect"), sym("am lit indirect"), sym("am lit")))

	g.WithAction("am push/pop", psec.Alt(litIC("push"), litIC("pop"),
		litIC("-[SP]"), litIC("[SP]+")),
		func(r interface{}, loc *psec.Loc) (interface{}, error) {
			return &spRel{adjustSP: true}, nil
		})

	g.WithAction("am peek", psec.Alt(litIC("peek"), litIC("[sp]")),
		func(r interface{}, loc *psec.Loc) (interface{}, error) {
			return &spRel{}, nil
		})

	g.AddSymbol("am special reg",
		psec.Alt(sym("pc"), sym("sp"), sym("ia"), sym("ex")))

	g.WithAction("+/- offset", psec.Seq(psec.OneOf("+-"), sym("wsline"), sym("expr")),
		func(r interface{}, loc *psec.Loc) (interface{}, error) {
			rs := r.([]interface{})
			expr := rs[2].(core.Expression)
			if rs[0].(byte) == '-' {
				expr = core.Unary(core.MINUS, expr)
			}

			return expr, nil
		})

	g.WithAction("am pc-rel",
		psec.SeqAt(3, lit("["), sym("wsline"), litIC("pc"),
			psec.Alt(psec.SeqAt(1, sym("comma"), sym("gpReg")),
				sym("+/- offset")),
			sym("wsline"), lit("]")),
		func(r interface{}, loc *psec.Loc) (interface{}, error) {
			// Either an Expression or a *regSimple.
			if expr, ok := r.(core.Expression); ok {
				return &pcRel{offset: expr}, nil
			}
			if reg, ok := r.(*regSimple); ok {
				return &pcRel{reg: reg.reg}, nil
			}
			return nil, fmt.Errorf("can't happen: pc-rel type mismatch")
		})

	g.WithAction("am sp-rel", psec.SeqAt(4, lit("["), sym("wsline"), litIC("sp"),
		sym("wsline"), sym("+/- offset"), sym("wsline"), lit("]")),
		func(r interface{}, loc *psec.Loc) (interface{}, error) {
			return &spRel{offset: r.(core.Expression)}, nil
		})

	g.WithAction("am reg indirect",
		psec.Seq(lit("["), sym("wsline"), sym("gpReg"), sym("wsline"),
			psec.Optional(psec.Alt(sym("+/- offset"), psec.SeqAt(1, sym("comma"), sym("gpReg")))),
			sym("wsline"), lit("]")),
		func(r interface{}, loc *psec.Loc) (interface{}, error) {
			rs := r.([]interface{})

			base := rs[2].(*regSimple)

			// The index is either nil, an Expression (offset), or a regSimple.
			if expr, ok := rs[4].(core.Expression); ok {
				return &regOffset{reg: base.reg, offset: expr}, nil
			}
			if index, ok := rs[4].(*regSimple); ok {
				return &regIndexed{reg: base.reg, index: index.reg}, nil
			}

			// Otherwise, it's base but with indirection.
			base.mode = rmIndirect
			return base, nil
		})

	// Used by the below to capture a simple [A] indirection.
	g.WithAction("just reg indirect",
		psec.SeqAt(2, lit("["), sym("wsline"), sym("gpReg"), sym("wsline"), lit("]")),
		func(r interface{}, loc *psec.Loc) (interface{}, error) {
			reg := r.(*regSimple)
			reg.mode = rmIndirect
			return reg, nil
		})

	g.WithAction("am reg increment",
		psec.Seq(psec.Optional(psec.SeqAt(0, lit("-"), sym("wsline"))),
			sym("just reg indirect"),
			psec.Optional(psec.SeqAt(1, sym("wsline"), lit("+")))),
		func(r interface{}, loc *psec.Loc) (interface{}, error) {
			rs := r.([]interface{})
			base := rs[1].(*regSimple)

			if predec, ok := rs[0].(string); ok && predec == "-" {
				base.mode = rmPredecrement
			} else if postinc, ok := rs[2].(string); ok && postinc == "+" {
				base.mode = rmPostincrement
			} else {
				base.mode = rmIndirect
			}
			return base, nil
		})

	g.WithAction("am lit indirect",
		psec.SeqAt(2, lit("["), sym("wsline"), sym("expr"), sym("wsline"), lit("]")),
		func(r interface{}, loc *psec.Loc) (interface{}, error) {
			return &immediate{value: r.(core.Expression), indirect: true}, nil
		})

	g.WithAction("am lit", sym("expr"),
		func(r interface{}, loc *psec.Loc) (interface{}, error) {
			return &immediate{value: r.(core.Expression)}, nil
		})
}

func addBinaryOpParsers(g *psec.Grammar) {
	g.AddSymbol("binary opcodes", psec.Alt(
		litIC("add"), litIC("adx"), litIC("sub"), litIC("sbx"), litIC("mul"),
		litIC("mli"), litIC("div"), litIC("dvi"), litIC("and"), litIC("bor"),
		litIC("xor"), litIC("shr"), litIC("shl"), litIC("asr")))

	g.WithAction("binary instruction", psec.Seq(sym("binary opcodes"),
		sym("suffix"), sym("ws1"), sym("operand"), sym("comma"), sym("operand")),
		func(r interface{}, loc *psec.Loc) (interface{}, error) {
			// Both parts will parse initially as fully operands, since we need more
			// complex logic to determine whether it can be encoded properly.
			rs := r.([]interface{})
			opcode := rs[0].(string)
			longwords := rs[1].(bool)
			dst := rs[3].(operand)
			src := rs[5].(operand)

			// ADD, SUB, AND, BOR and XOR have an "immediate" form, so they can
			// support both arguments being complex operands.
			if isDirectReg(dst) {
				return binaryOp(binaryOpcodes[opcode], src, dst.(*regSimple).reg,
					false, longwords), nil
			}
			if isDirectReg(src) {
				return binaryOp(binaryOpcodes[opcode], dst, src.(*regSimple).reg,
					true, longwords), nil
			}

			// If the src value is a literal, we can use the immediate form.
			if lit, ok := src.(*immediate); ok && !lit.indirect {
				// Only some operations are supported, so check.
				op, ok := immediateOpcodes[opcode]
				if ok {
					return immediateOp(op, dst, lit.value, longwords), nil
				}
			}

			// Otherwise, this is not a legal combination.
			return nil, fmt.Errorf("cannot assemble %s %s, %s, one argument must be a register", opcode, dst.ErrLabel(), src.ErrLabel())
		})
}

func addBitTwiddlerParsers(g *psec.Grammar) {
	g.AddSymbol("twiddler opcode", alts("btx", "bts", "btc", "btm"))

	g.WithAction("twiddler instruction",
		psec.Seq(sym("twiddler opcode"), sym("suffix"), sym("ws1"),
			sym("operand"), sym("comma"), psec.Alt(sym("gpReg"), sym("expr"))),
		func(r interface{}, loc *psec.Loc) (interface{}, error) {
			rs := r.([]interface{})
			op := rs[0].(string)
			longwords := rs[1].(bool)
			dst := rs[3].(operand)

			opcode := bitTwiddlerOpcodes[op]
			expr, ok := rs[5].(core.Expression)
			if !ok {
				// Abusing the literal form.
				expr = &core.Constant{Value: uint32(rs[5].(*regSimple).reg)}
				opcode |= 4 // The register versions are above the immediate ones.
			}

			return &bitTwiddlerOp{opcode: opcode, longwords: longwords, dst: dst, bit: expr}, nil
		})
}

func addUnaryOpParsers(g *psec.Grammar) {
	g.AddSymbol("unary basic opcodes", alts("swp", "pea", "ext", "int"))
	g.AddSymbol("unary suffixed opcodes", alts("not", "neg", "jsr", "iaq", "log", "hwi"))

	g.WithAction("unary basic instruction", psec.Seq(
		sym("unary basic opcodes"), sym("ws1"), sym("operand")),
		func(r interface{}, loc *psec.Loc) (interface{}, error) {
			rs := r.([]interface{})
			op := rs[0].(string)
			dst := rs[2].(operand)

			if op == "pea" && !dst.HasEffectiveAddress() {
				return nil, fmt.Errorf("cannot use PEA with %s; it has no address",
					dst.ErrLabel())
			}

			return &unaryOp{
				opcode: unaryOpcodes[rs[0].(string)],
				dst:    rs[2].(operand),
			}, nil
		})

	g.WithAction("unary suffixed instruction", psec.Seq(
		sym("unary suffixed opcodes"), sym("suffix"), sym("ws1"), sym("operand")),
		func(r interface{}, loc *psec.Loc) (interface{}, error) {
			rs := r.([]interface{})
			opcode := unaryOpcodes[rs[0].(string)]
			if rs[1].(bool) {
				opcode |= 1 // Mix in the L bit.
			}

			return &unaryOp{opcode: opcode, dst: rs[3].(operand)}, nil
		})

	g.AddSymbol("unary instruction", psec.Alt(
		sym("unary basic instruction"), sym("unary suffixed instruction")))
}

func addRegOpParsers(g *psec.Grammar) {
	g.AddSymbol("reg instruction",
		psec.Alt(sym("lnk instruction"), sym("reg instruction 2")))

	g.AddSymbol("reg opcodes", alts("ulk", "hwn", "hwq"))
	g.WithAction("reg instruction 2", psec.Seq(
		sym("reg opcodes"), sym("ws1"), sym("gpReg")),
		func(r interface{}, loc *psec.Loc) (interface{}, error) {
			rs := r.([]interface{})
			opcode := regOpcodes[rs[0].(string)]
			reg := rs[2].(*regSimple)
			return &regOp{opcode: opcode, reg: reg.reg}, nil
		})

	g.WithAction("lnk instruction", psec.Seq(
		litIC("lnk"), sym("ws1"), sym("gpReg"), sym("comma"), sym("expr")),
		func(r interface{}, loc *psec.Loc) (interface{}, error) {
			rs := r.([]interface{})
			reg := rs[2].(*regSimple)
			expr := rs[4].(core.Expression)
			return &regOp{opcode: regOpcodes["lnk"], reg: reg.reg, linkWord: expr}, nil
		})
}

func addNullaryOpParsers(g *psec.Grammar) {
	g.WithAction("nullary instruction", alts("nop", "rfi", "brk", "hlt", "ret"),
		func(r interface{}, loc *psec.Loc) (interface{}, error) {
			return &nullaryOp{opcode: nullaryOpcodes[r.(string)]}, nil
		})
}

func addBranchOpParsers(g *psec.Grammar) {
	addUnaryBranchParsers(g)
	addBinaryBranchParsers(g)

	g.AddSymbol("branch instruction",
		psec.Alt(sym("unary branch instruction"), sym("unary skip instruction"),
			sym("binary branch instruction"), sym("binary skip instruction")))
}

func addUnaryBranchParsers(g *psec.Grammar) {
	g.AddSymbol("unary branch opcode",
		alts("bzrd", "bnzd", "bngd", "bpsd", "bzr", "bnz", "bpo", "bng"))

	g.WithAction("unary branch instruction", psec.Seq(
		sym("unary branch opcode"), sym("suffix"), sym("ws1"), sym("operand"),
		sym("comma"), sym("expr")),
		func(r interface{}, loc *psec.Loc) (interface{}, error) {
			rs := r.([]interface{})
			return &unaryBranchOp{
				opcode:    unaryBranchOpcodes[rs[0].(string)],
				longwords: rs[1].(bool),
				dst:       rs[3].(operand),
				target:    rs[5].(core.Expression),
			}, nil
		})

	g.AddSymbol("unary skip opcode",
		alts("szrd", "snzd", "sngd", "spsd", "szr", "snz", "sps", "sng"))
	g.WithAction("unary skip instruction", psec.Seq(
		sym("unary skip opcode"), sym("suffix"), sym("ws1"), sym("operand")),
		func(r interface{}, loc *psec.Loc) (interface{}, error) {
			rs := r.([]interface{})
			return &unaryBranchOp{
				opcode:    unaryBranchOpcodes[rs[0].(string)],
				longwords: rs[1].(bool),
				dst:       rs[3].(operand),
			}, nil
		})
}

func addBinaryBranchParsers(g *psec.Grammar) {
	g.AddSymbol("binary branch opcode",
		alts("brb", "brc", "bre", "brn", "brg", "bra", "brl", "bru"))
	g.WithAction("binary branch instruction", psec.Seq(
		sym("binary branch opcode"), sym("suffix"), sym("ws1"),
		sym("operand"), sym("comma"), sym("operand"), sym("comma"), sym("expr")),
		func(r interface{}, loc *psec.Loc) (interface{}, error) {
			rs := r.([]interface{})
			return &binaryBranchOp{
				opcode:    binaryBranchOpcodes[rs[0].(string)],
				longwords: rs[1].(bool),
				left:      rs[3].(operand),
				right:     rs[5].(operand),
				target:    rs[7].(core.Expression),
			}, nil
		})

	g.AddSymbol("binary skip opcode",
		alts("ifb", "ifc", "ife", "ifn", "ifg", "ifa", "ifl", "ifu"))
	g.WithAction("binary skip instruction", psec.Seq(
		sym("binary skip opcode"), sym("suffix"), sym("ws1"),
		sym("operand"), sym("comma"), sym("operand")),
		func(r interface{}, loc *psec.Loc) (interface{}, error) {
			rs := r.([]interface{})
			return &binaryBranchOp{
				opcode:    binaryBranchOpcodes[rs[0].(string)],
				longwords: rs[1].(bool),
				left:      rs[3].(operand),
				right:     rs[5].(operand),
			}, nil
		})
}

func addSetOpParsers(g *psec.Grammar) {
	g.WithAction("set instruction", psec.Seq(
		litIC("set"), sym("suffix"), sym("ws1"), sym("operand"), sym("comma"), sym("operand")),
		func(r interface{}, loc *psec.Loc) (interface{}, error) {
			rs := r.([]interface{})
			return &setOp{
				longwords: rs[1].(bool),
				src:       rs[5].(operand),
				dst:       rs[3].(operand),
			}, nil
		})

	g.WithAction("lea instruction", psec.Seq(
		litIC("lea"), sym("ws1"), sym("gpReg"), sym("comma"), sym("operand")),
		func(r interface{}, loc *psec.Loc) (interface{}, error) {
			rs := r.([]interface{})
			src := rs[4].(operand)
			if !src.HasEffectiveAddress() {
				return nil, fmt.Errorf("cannot use LEA with %s; it has no address",
					src.ErrLabel())
			}

			return &leaOp{reg: rs[2].(*regSimple).reg, src: src}, nil
		})
}
