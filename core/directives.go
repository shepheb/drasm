package core

import "github.com/shepheb/psec"

// Shared psec parsers for the assembler directives.
func addDirectiveParsers(g *psec.Grammar) {
	addMacroParsers(g)
	g.WithAction("dir:org",
		psec.SeqAt(2, litIC("org"), sym("ws1"), sym("expr")),
		func(r interface{}, loc *psec.Loc) (interface{}, error) {
			return &Org{Abs: r.(Expression)}, nil
		})
	g.WithAction("dir:fill",
		psec.Seq(litIC("fill"), sym("ws1"), sym("expr"),
			ws(), lit(","), ws(), sym("expr")),
		func(r interface{}, loc *psec.Loc) (interface{}, error) {
			// Value, then Length.
			rs := r.([]interface{})
			return &FillBlock{Value: rs[2].(Expression), Length: rs[6].(Expression)}, nil
		})
	g.WithAction("dir:reserve",
		psec.SeqAt(2, litIC("reserve"), sym("ws1"), sym("expr")),
		func(r interface{}, loc *psec.Loc) (interface{}, error) {
			// Just the length
			return &FillBlock{Value: &Constant{Value: 0}, Length: r.(Expression)}, nil
		})

	g.WithAction("dir:include",
		psec.SeqAt(2, litIC("include"), sym("ws1"), sym("string")),
		func(r interface{}, loc *psec.Loc) (interface{}, error) {
			// Recursively parse the file.
			return currentDriver.ParseFile(r.(string))
		})
	g.WithAction("dir:symbol",
		psec.Seq(psec.Alt(litIC("symbol"), litIC("sym"), litIC("equ"),
			litIC("set"), litIC("define"), litIC("def")),
			sym("ws1"), sym("identifier"), ws(), lit(","), ws(), sym("expr")),
		func(r interface{}, loc *psec.Loc) (interface{}, error) {
			rs := r.([]interface{})
			return DefineSymbol(rs[2].(string), rs[6].(Expression)), nil
		})
	g.WithAction("dir:dat",
		psec.SeqAt(2, litIC("dat"), sym("ws1"),
			psec.SepBy(psec.Alt(sym("string"), sym("expr")), psec.Seq(ws(), lit(","), ws()))),
		func(r interface{}, loc *psec.Loc) (interface{}, error) {
			var values []Expression
			for _, value := range r.([]interface{}) {
				if expr, ok := value.(Expression); ok {
					values = append(values, expr.(Expression))
				} else if s, ok := value.(string); ok {
					// Write each byte from the string into our DAT as a Constant.
					for _, b := range s {
						values = append(values, &Constant{Value: uint32(b)})
					}
				}
			}
			return &DatBlock{Values: values}, nil
		})
	g.WithAction("dir:macro",
		psec.Seq(litIC("macro"), sym("wsline"), sym("identifier"), sym("wsline"),
			lit("="), psec.Stringify(psec.Many1(psec.NoneOf("\n")))),
		func(r interface{}, loc *psec.Loc) (interface{}, error) {
			rs := r.([]interface{})
			ident := rs[2].(string)
			body := rs[5].(string)
			addMacro(ident, body)
			return &MacroDef{name: ident, body: body}, nil
		})

	g.AddSymbol("directive",
		psec.SeqAt(1, psec.Literal("."),
			psec.Alt(sym("dir:fill"), sym("dir:reserve"), sym("dir:include"),
				sym("dir:macro"), sym("dir:org"), sym("dir:dat"), sym("dir:symbol"))))
}
