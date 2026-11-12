package pdftext

import (
	"math"
	"strings"

	"github.com/ledongthuc/pdf"
)

// This file ports the content-stream walker and PlainTextOutput from
// pdf-extract 0.12.1. rustTextExtract below corresponds to
// finglass-core's documents::pdf_text::extract_text_inner.

// Extract reads the text of at most maxPages pages.
//
// It returns ok=false when the PDF cannot be loaded or parsed. A malformed PDF
// never panics, even when the underlying reader does.
func Extract(path string, maxPages int) (text *Text, ok bool) {
	defer func() {
		if recover() != nil {
			text = nil
			ok = false
		}
	}()

	if !withinSizeLimit(path) {
		return nil, false
	}

	file, reader, err := pdf.Open(path)
	if err != nil {
		return nil, false
	}
	defer func() { _ = file.Close() }()

	pageCount := reader.NumPage()
	limit := pageCount
	if maxPages < limit {
		limit = maxPages
	}
	if limit < 0 {
		limit = 0
	}

	var builder strings.Builder
	for pageNumber := 1; pageNumber <= limit; pageNumber++ {
		page := reader.Page(pageNumber)
		if page.V.IsNull() {
			panic("missing page")
		}

		llx, lly, urx, ury := pageMediaBox(page)
		_ = llx
		_ = urx

		output := newPlainTextOutput(&builder, ury-lly)
		processor := newProcessor()

		contents := page.V.Key("Contents")
		if contents.Kind() != pdf.Null {
			processor.processStream(contents, page.Resources(), output, 0)
		}
	}

	return &Text{Text: strings.TrimSpace(builder.String()), PageCount: pageCount}, true
}

func pageMediaBox(page pdf.Page) (llx, lly, urx, ury float64) {
	for value := page.V; !value.IsNull(); value = value.Key("Parent") {
		box := value.Key("MediaBox")
		if box.Kind() != pdf.Array {
			continue
		}
		// pdf-extract converts the whole array to Vec<f64>, so a non-number
		// element is a failure even when the first four numbers are fine.
		for i := 0; i < box.Len(); i++ {
			_ = asNum(box.Index(i))
		}
		return asNum(box.Index(0)), asNum(box.Index(1)), asNum(box.Index(2)), asNum(box.Index(3))
	}
	panic("MediaBox")
}

// plainTextOutput ports PlainTextOutput::output_character and its state.
type plainTextOutput struct {
	builder   *strings.Builder
	lastEnd   float64
	lastY     float64
	firstChar bool
	flipCtm   matrix
}

func newPlainTextOutput(builder *strings.Builder, pageHeight float64) *plainTextOutput {
	return &plainTextOutput{
		builder:   builder,
		lastEnd:   100000,
		firstChar: false,
		lastY:     0,
		flipCtm:   rowMajor(1, 0, 0, -1, 0, pageHeight),
	}
}

func (o *plainTextOutput) beginWord() {
	o.firstChar = true
}

func (o *plainTextOutput) endWord() {}
func (o *plainTextOutput) endLine() {}

func (o *plainTextOutput) outputCharacter(trm *matrix, width, _ float64, fontSize float64, char string) {
	position := trm.postTransform(o.flipCtm)
	vx, vy := trm.transformVector(fontSize, fontSize)
	transformedFontSize := math.Sqrt(vx * vy)
	x, y := position.m31, position.m32

	if o.firstChar {
		if math.Abs(y-o.lastY) > transformedFontSize*1.5 {
			o.builder.WriteByte('\n')
		}
		if x < o.lastEnd && math.Abs(y-o.lastY) > transformedFontSize*0.5 {
			o.builder.WriteByte('\n')
		}
		if x > o.lastEnd+transformedFontSize*0.1 {
			o.builder.WriteByte(' ')
		}
	}

	o.builder.WriteString(char)
	o.firstChar = false
	o.lastY = y
	o.lastEnd = x + width*transformedFontSize
}

// --- content-stream processor -------------------------------------------

type textState struct {
	font              pdfFont
	fontSize          float64
	characterSpacing  float64
	wordSpacing       float64
	horizontalScaling float64
	leading           float64
	rise              float64
	tm                matrix
}

type graphicsState struct {
	ctm matrix
	ts  textState
}

type processor struct {
	fontTable map[string]pdfFont
}

func newProcessor() *processor {
	return &processor{fontTable: make(map[string]pdfFont)}
}

func popArgs(stack *pdf.Stack) []pdf.Value {
	n := stack.Len()
	args := make([]pdf.Value, n)
	for i := n - 1; i >= 0; i-- {
		args[i] = stack.Pop()
	}
	return args
}

func (p *processor) processStream(content pdf.Value, resources pdf.Value, output *plainTextOutput, depth int) {
	if depth > 64 {
		panic("form XObject nesting too deep")
	}

	gs := graphicsState{
		ctm: identityMatrix(),
		ts: textState{
			fontSize:          math.NaN(),
			horizontalScaling: 1,
			tm:                identityMatrix(),
		},
	}
	var stateStack []graphicsState
	tlm := identityMatrix()

	pdf.Interpret(content, func(stack *pdf.Stack, operator string) {
		args := popArgs(stack)

		switch operator {
		case "BT", "ET":
			tlm = identityMatrix()
			gs.ts.tm = tlm
		case "cm":
			if len(args) != 6 {
				panic("bad cm operands")
			}
			m := rowMajor(
				asNum(args[0]), asNum(args[1]),
				asNum(args[2]), asNum(args[3]),
				asNum(args[4]), asNum(args[5]),
			)
			gs.ctm = gs.ctm.preTransform(m)
		case "TJ":
			if len(args) < 1 {
				panic("bad TJ operands")
			}
			if args[0].Kind() != pdf.Array {
				return
			}
			array := args[0]
			for i := 0; i < array.Len(); i++ {
				element := array.Index(i)
				switch element.Kind() {
				case pdf.String:
					showText(&gs, []byte(element.RawString()), output)
				case pdf.Integer, pdf.Real:
					tj := asNum(element)
					tx := gs.ts.horizontalScaling * ((0 - tj/1000) * gs.ts.fontSize)
					gs.ts.tm = gs.ts.tm.preTransform(translation(tx, 0))
				}
			}
		case "Tj":
			if len(args) < 1 || args[0].Kind() != pdf.String {
				panic("unexpected Tj operand")
			}
			showText(&gs, []byte(args[0].RawString()), output)
		case "Tc":
			gs.ts.characterSpacing = asNum(firstArg(args))
		case "Tw":
			gs.ts.wordSpacing = asNum(firstArg(args))
		case "Tz":
			gs.ts.horizontalScaling = asNum(firstArg(args)) / 100
		case "TL":
			gs.ts.leading = asNum(firstArg(args))
		case "Tf":
			if len(args) < 2 {
				panic("bad Tf operands")
			}
			fontName := nameOf(args[0])
			fonts := resources.Key("Font")
			if fonts.Kind() != pdf.Dict {
				panic("missing font dictionary")
			}
			font, ok := p.fontTable[fontName]
			if !ok {
				fontValue := fonts.Key(fontName)
				if fontValue.Kind() != pdf.Dict {
					panic("missing font")
				}
				font = makeFont(fontValue)
				p.fontTable[fontName] = font
			}
			gs.ts.font = font
			gs.ts.fontSize = asNum(args[1])
		case "Ts":
			gs.ts.rise = asNum(firstArg(args))
		case "Tm":
			if len(args) != 6 {
				panic("bad Tm operands")
			}
			tlm = rowMajor(
				asNum(args[0]), asNum(args[1]),
				asNum(args[2]), asNum(args[3]),
				asNum(args[4]), asNum(args[5]),
			)
			gs.ts.tm = tlm
			output.endLine()
		case "Td":
			if len(args) != 2 {
				panic("bad Td operands")
			}
			tlm = tlm.preTransform(translation(asNum(args[0]), asNum(args[1])))
			gs.ts.tm = tlm
			output.endLine()
		case "TD":
			if len(args) != 2 {
				panic("bad TD operands")
			}
			tx := asNum(args[0])
			ty := asNum(args[1])
			gs.ts.leading = -ty
			tlm = tlm.preTransform(translation(tx, ty))
			gs.ts.tm = tlm
			output.endLine()
		case "T*":
			tlm = tlm.preTransform(translation(0, -gs.ts.leading))
			gs.ts.tm = tlm
			output.endLine()
		case "q":
			stateStack = append(stateStack, gs)
		case "Q":
			if n := len(stateStack); n > 0 {
				gs = stateStack[n-1]
				stateStack = stateStack[:n-1]
			}
		case "Do":
			if len(args) < 1 {
				panic("bad Do operands")
			}
			xobjects := resources.Key("XObject")
			if xobjects.Kind() != pdf.Dict {
				panic("missing XObject dictionary")
			}
			xobject := xobjects.Key(nameOf(args[0]))
			if xobject.Kind() != pdf.Stream {
				panic("missing XObject")
			}
			childResources := xobject.Key("Resources")
			if childResources.Kind() != pdf.Dict {
				childResources = resources
			}
			p.processStream(xobject, childResources, output, depth+1)
		}
	})
}

func firstArg(args []pdf.Value) pdf.Value {
	if len(args) < 1 {
		panic("missing operand")
	}
	return args[0]
}

func nameOf(value pdf.Value) string {
	if value.Kind() != pdf.Name {
		panic("expected name operand")
	}
	return value.Name()
}

func showText(gs *graphicsState, s []byte, output *plainTextOutput) {
	font := gs.ts.font
	if font == nil {
		panic("text shown before Tf")
	}
	output.beginWord()

	iter := &charIter{data: s}
	for {
		char, length, ok := font.nextChar(iter)
		if !ok {
			break
		}

		tsm := rowMajor(gs.ts.horizontalScaling, 0, 0, 1, 0, gs.ts.rise)
		trm := tsm.postTransform(gs.ts.tm.postTransform(gs.ctm))

		w0 := font.getWidth(char) / 1000

		spacing := gs.ts.characterSpacing
		if char == 32 && length == 1 {
			spacing += gs.ts.wordSpacing
		}

		output.outputCharacter(&trm, w0, spacing, gs.ts.fontSize, font.decodeChar(char))

		tx := gs.ts.horizontalScaling * ((w0-0.0/1000)*gs.ts.fontSize + spacing)
		gs.ts.tm = gs.ts.tm.preTransform(translation(tx, 0))
	}

	output.endWord()
}
