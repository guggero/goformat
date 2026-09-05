package format

import (
	"bytes"
	"fmt"
	"go/scanner"
	"go/token"
	"strings"

	"github.com/dave/dst"
)

// sourceRange includes complete source lines, with an exclusive end offset.
type sourceRange struct {
	start, end int
}

// noformatRanges follows lexical delimiters rather than AST nodes: a directive
// can protect a statement, a field, or just one line of a larger expression.
// Scanning tokens also keeps braces in strings and comments out of the count.
func noformatRanges(src []byte) []sourceRange {
	fset := token.NewFileSet()
	file := fset.AddFile("", -1, len(src))
	var scan scanner.Scanner
	scan.Init(file, src, nil, scanner.ScanComments)
	type lexeme struct {
		start, end int
		kind       token.Token
	}
	var tokens []lexeme
	var directives []int
	for {
		pos, kind, lit := scan.Scan()
		if kind == token.EOF {
			break
		}
		off := file.Offset(pos)
		end := off + len(lit)
		if lit == "" {
			end = off + len(kind.String())
		}

		// The scanner removes carriage returns from raw literals and
		// comments. Locate their actual terminators to keep offsets in
		// the original bytes.
		if kind == token.STRING && src[off] == '`' {
			if close := bytes.IndexByte(
				src[off+1:], '`',
			); close >= 0 {

				end = off + close + 2
			}
		}
		if kind == token.COMMENT && strings.HasPrefix(lit, "/*") {
			if close := bytes.Index(src[off+2:], []byte(
				"*/",
			)); close >= 0 {

				end = off + close + 4
			}
		}
		tokens = append(tokens, lexeme{off, end, kind})
		if kind == token.COMMENT && strings.HasPrefix(lit, "//") &&
			strings.TrimSpace(lit[2:]) == "noformat" {

			start := lineStart(src, off)
			if len(bytes.TrimSpace(src[start:off])) == 0 {
				directives = append(directives, start)
			}
		}
	}
	var ranges []sourceRange
	for _, start := range directives {
		next := lineEnd(src, start)
		end := lineEnd(src, next)
		var stack []token.Token
		for _, tok := range tokens {
			if tok.start < next {
				continue
			}
			if tok.start >= end && len(stack) == 0 {
				break
			}
			if tok.end > end {
				end = lineEnd(src, tok.end-1)
			}
			switch tok.kind {
			case token.LPAREN, token.LBRACK, token.LBRACE:
				stack = append(stack, tok.kind)

			case token.RPAREN, token.RBRACK, token.RBRACE:
				if len(stack) > 0 {
					stack = stack[:len(stack)-1]
				}
			}
		}
		if len(ranges) > 0 && start <= ranges[len(ranges)-1].end {
			if end > ranges[len(ranges)-1].end {
				ranges[len(ranges)-1].end = end
			}
		} else {
			ranges = append(ranges, sourceRange{start, end})
		}
	}
	return ranges
}

func lineStart(src []byte, off int) int {
	return bytes.LastIndexByte(src[:off], '\n') + 1
}

func lineEnd(src []byte, off int) int {
	if i := bytes.IndexByte(src[off:], '\n'); i >= 0 {
		return off + i + 1
	}
	return len(src)
}

// protectedSource sandwiches each region between unique comments. The AST
// passes skip protected nodes; restoration after printing additionally undoes
// gofmt's whitespace changes, preserving the original bytes verbatim.
type protectedSource struct {
	raw        []byte
	begin, end string
	lines      lineRange
	blankAfter bool
}

func protectSource(src []byte) ([]byte, []protectedSource) {
	ranges := noformatRanges(src)
	if len(ranges) == 0 {
		return src, nil
	}
	prefix := "//noformat:goformat:"
	for bytes.Contains(src, []byte(prefix)) {
		prefix += "x:"
	}
	var out bytes.Buffer
	var protected []protectedSource
	prev := 0
	for i, r := range ranges {
		out.Write(src[prev:r.start])
		p := protectedSource{
			raw: src[r.start:r.end],
			blankAfter: r.end < len(
				src,
			) && len(bytes.TrimSpace(
				src[r.end:lineEnd(src, r.end)],
			)) == 0,
			begin: fmt.Sprintf("%s%d:begin", prefix, i),
			end:   fmt.Sprintf("%s%d:end", prefix, i),
		}
		p.lines.start = bytes.Count(out.Bytes(), []byte("\n")) + 1

		// Separate markers from source comment groups. The Go printer
		// moves directives to the end of a Godoc group; without a
		// boundary, that can move protected comments outside the region
		// being restored.
		out.WriteString(p.begin + "\n\n")
		out.Write(p.raw)
		if !bytes.HasSuffix(p.raw, []byte("\n")) {
			out.WriteByte('\n')
		}
		out.WriteByte('\n')
		p.lines.end = bytes.Count(out.Bytes(), []byte("\n")) + 1
		out.WriteString(p.end + "\n")
		protected = append(protected, p)
		prev = r.end
	}
	out.Write(src[prev:])
	return out.Bytes(), protected
}

func restoreSource(src []byte, protected []protectedSource) ([]byte, error) {
	for _, p := range protected {
		begin := bytes.Index(src, []byte(p.begin))
		end := bytes.Index(src, []byte(p.end))
		if begin < 0 || end < begin {
			return nil, fmt.Errorf("noformat: printer lost " +
				"protected region")
		}
		start := lineStart(src, begin)
		stop := lineEnd(src, end)
		out := make([]byte, 0, len(src))
		out = append(out, src[:start]...)
		out = append(out, p.raw...)
		if p.blankAfter && stop < len(src) && src[stop] != '\n' {
			out = append(out, '\n')
		}
		out = append(out, src[stop:]...)
		src = out
	}
	return src, nil
}

func (ctx *Context) protectedNode(n dst.Node) bool {
	if _, ok := n.(*dst.File); ok {
		return false
	}
	original := ctx.Decorator.Ast.Nodes[n]
	if original == nil {
		return false
	}
	start := ctx.FileSet.PositionFor(original.Pos(), false).Line
	end := ctx.FileSet.PositionFor(original.End(), false).Line
	for _, p := range ctx.Protected {
		if start >= p.lines.start && end <= p.lines.end {
			return true
		}

		// Wrapping a containing expression could move the markers or
		// change the indentation context of the exact source we
		// restore.
		switch n.(type) {
		case *dst.CallExpr, *dst.CompositeLit, *dst.BinaryExpr:
			if start <= p.lines.end && end >= p.lines.start {
				return true
			}
		}
	}
	return false
}
