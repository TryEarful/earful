package http_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// meteredAIMethods are the ai.Provider operations that cost money per call
// and must therefore be gated by the budget breaker.
var meteredAIMethods = map[string]bool{
	"Generate": true, "Transcribe": true, "Translate": true, "Analyze": true,
}

// TestAIProviderCallsAreMetered is a build-time guardrail (security audit
// C2). ai.Meter enforces the per-workspace daily token cap and the global
// daily € breaker, but it only protects the bill if every ai.Provider call
// is preceded by aiMeter.Check in the same function. No handler calls the
// provider today, so this test passes; it exists to FAIL the build the
// moment a metered operation (Generate/Transcribe/Translate/Analyze) on
// the server's `ai` field is wired up without gating it through the meter.
//
// It matches the direct `s.ai.Generate(...)` / `s.aiMeter.Check(...)`
// shape by receiver name; routing a provider call through a local alias
// would slip past it, but the idiomatic call site is the one this catches.
func TestAIProviderCallsAreMetered(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			var providerCalls []string
			metered := false
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				switch {
				case meteredAIMethods[sel.Sel.Name] && trailingIdent(sel.X) == "ai":
					providerCalls = append(providerCalls, sel.Sel.Name)
				case sel.Sel.Name == "Check" && trailingIdent(sel.X) == "aiMeter":
					metered = true
				}
				return true
			})
			if len(providerCalls) > 0 && !metered {
				t.Errorf("%s: %s calls ai.Provider %v without a preceding aiMeter.Check — "+
					"every metered AI call must go through the budget breaker (security audit C2)",
					fset.Position(fn.Pos()), fn.Name.Name, providerCalls)
			}
		}
	}
}

// trailingIdent returns the final identifier of a selector/ident chain, so
// the receiver of a call (e.g. "ai" in s.ai.Generate) can be matched.
func trailingIdent(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.SelectorExpr:
		return v.Sel.Name
	case *ast.StarExpr:
		return trailingIdent(v.X)
	case *ast.ParenExpr:
		return trailingIdent(v.X)
	default:
		return ""
	}
}
