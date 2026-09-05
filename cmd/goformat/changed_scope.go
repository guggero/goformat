package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
)

// formattingScopes selects the smallest formatting unit owning each changed
// line. A statement can extend beyond the Git hunk, but another statement in
// the same function does not become eligible merely because it shares a file.
func formattingScopes(src []byte, changed []lineSpan) ([]lineSpan, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", src, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	span := func(start, end token.Pos) lineSpan {
		return lineSpan{fset.PositionFor(start, false).Line - 1,
			fset.PositionFor(end, false).Line}
	}
	var units []lineSpan
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		start := gd.Pos()
		if gd.Doc != nil {
			start = gd.Doc.Pos()
		}
		s := span(start, gd.End())
		units = append(units, s)

	}
	var parents []ast.Node
	ast.Inspect(file, func(n ast.Node) bool {
		if n == nil {
			parents = parents[:len(parents)-1]
			return true
		}
		var parent ast.Node
		if len(parents) > 0 {
			parent = parents[len(parents)-1]
		}
		parents = append(parents, n)
		start, end := n.Pos(), n.End()
		selectNode := true
		switch x := n.(type) {
		case *ast.AssignStmt:
			switch parent.(type) {
			case *ast.IfStmt, *ast.ForStmt, *ast.SwitchStmt, *ast.TypeSwitchStmt:
				selectNode = false
			}

		case *ast.ExprStmt, *ast.ReturnStmt,
			*ast.DeclStmt, *ast.GoStmt, *ast.DeferStmt,
			*ast.SendStmt, *ast.IncDecStmt, *ast.BranchStmt,
			*ast.TypeSpec, *ast.ValueSpec, *ast.CommentGroup:

		case *ast.FuncDecl:
			if x.Body != nil {
				end = x.Body.Lbrace
			}

		case *ast.FuncLit:
			end = x.Body.Lbrace

		case *ast.IfStmt:
			end = x.Body.Lbrace

		case *ast.ForStmt:
			end = x.Body.Lbrace

		case *ast.RangeStmt:
			end = x.Body.Lbrace

		case *ast.SwitchStmt:
			end = x.Body.Lbrace

		case *ast.TypeSwitchStmt:
			end = x.Body.Lbrace

		case *ast.SelectStmt:
			end = x.Body.Lbrace

		case *ast.CaseClause:
			end = x.Colon

		case *ast.CommClause:
			end = x.Colon

		case *ast.Field:
			// Parameters and results belong to the whole signature.
			selectNode = false
			if _, ok := parent.(*ast.FieldList); ok &&
				len(parents) >= 3 {

				switch parents[len(parents)-3].(type) {
				case *ast.StructType, *ast.InterfaceType:
					selectNode = true
				}
			}

		default:
			selectNode = false
		}
		if selectNode {
			units = append(units, span(start, end))
		}
		return true
	})
	var scopes []lineSpan
	for _, dirty := range changed {
		if dirty.start == dirty.end {
			// A deleted argument or field still belongs to its
			// enclosing unit. A deleted whole declaration leaves no
			// unit spanning the gap.
			best := lineSpan{-1, -1}
			for _, unit := range units {
				if unit.start < dirty.start && unit.end > dirty.start && (best.start < 0 || unit.end-unit.start < best.end-best.start) {
					best = unit
				}
			}
			if best.start >= 0 {
				scopes = append(scopes, best)
			}
			continue
		}
		scopes = append(scopes, dirty)
		for line := dirty.start; line < dirty.end; line++ {
			best := lineSpan{-1, -1}
			for _, unit := range units {
				if unit.start <= line && unit.end > line && (best.start < 0 || unit.end-unit.start < best.end-best.start) {
					best = unit
				}
			}
			if best.start >= 0 {
				scopes = append(scopes, best)
			}
		}
	}
	sort.Slice(scopes, func(i, j int) bool {
		return scopes[i].start < scopes[j].start
	})
	var merged []lineSpan
	for _, s := range scopes {
		if len(merged) == 0 || s.start > merged[len(merged)-1].end {
			merged = append(merged, s)
		} else if s.end > merged[len(merged)-1].end {
			merged[len(merged)-1].end = s.end
		}
	}
	return merged, nil
}
