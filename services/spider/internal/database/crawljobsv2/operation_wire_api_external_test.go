package crawljobsv2_test

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	crawljobsv2 "github.com/IonelPopJara/search-engine/services/spider/internal/database/crawljobsv2"
)

func TestScriptBindingSetProductionAPIIsOpaqueAndFailClosed(t *testing.T) {
	for _, value := range []any{crawljobsv2.ScriptBindingSet{}, crawljobsv2.ScriptBindingReview{}} {
		typeOf := reflect.TypeOf(value)
		for index := 0; index < typeOf.NumField(); index++ {
			if typeOf.Field(index).IsExported() {
				t.Fatalf("%s exposes caller-settable field %q", typeOf.Name(), typeOf.Field(index).Name)
			}
		}
	}

	if _, err := crawljobsv2.BuildEvalSHARequest(
		crawljobsv2.ScriptBindingSet{}, crawljobsv2.OperationWireRequest{},
	); !errors.Is(err, crawljobsv2.ErrInvalidScriptBindingSet) {
		t.Fatalf("zero/unsealed bundle error = %v", err)
	}

	assertNoCallerInputScriptBundleFactory(t)
}

func TestRunPolicyAuthorityProductionAPIIsOpaqueAndFailClosed(t *testing.T) {
	authorityType := reflect.TypeOf(crawljobsv2.RunPolicyAuthority{})
	for index := 0; index < authorityType.NumField(); index++ {
		if authorityType.Field(index).IsExported() {
			t.Fatalf("RunPolicyAuthority exposes caller-settable field %q", authorityType.Field(index).Name)
		}
	}
	bindingsType := reflect.TypeOf(crawljobsv2.RunPinnedPolicyBindings{})
	if _, exists := bindingsType.FieldByName("PolicyGroups"); exists {
		t.Fatal("RunPinnedPolicyBindings lets callers replace the run-pinned policy-group map")
	}
	if err := crawljobsv2.ValidateRunPinnedPolicyBindings(
		crawljobsv2.RunPolicyAuthority{}, crawljobsv2.RunPinnedPolicyBindings{},
	); !errors.Is(err, crawljobsv2.ErrInvalidResponseAuthority) {
		t.Fatalf("zero run-policy authority error = %v", err)
	}
}

// assertNoCallerInputScriptBundleFactory inventories the compiled production
// API. A future fixed, zero-argument authoritative-bundle accessor is allowed;
// an exported function or method that accepts caller input and returns a bundle
// is not.
func assertNoCallerInputScriptBundleFactory(t *testing.T) {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate API regression test")
	}
	directory := filepath.Dir(currentFile)
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	files := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(files, filepath.Join(directory, name), nil, 0)
		if err != nil {
			t.Fatalf("parse production API file %s: %v", name, err)
		}
		for _, declaration := range parsed.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || !function.Name.IsExported() {
				continue
			}
			if function.Name.Name == "NewScriptBindingSet" {
				t.Fatalf("production API restored self-attesting constructor %s", function.Name.Name)
			}
			if returnsNamedType(function.Type.Results, "ScriptBindingSet") &&
				(function.Recv != nil || fieldCount(function.Type.Params) != 0) {
				t.Fatalf("exported %s accepts caller input or a receiver and returns ScriptBindingSet", function.Name.Name)
			}
		}
	}
}

func returnsNamedType(fields *ast.FieldList, name string) bool {
	if fields == nil {
		return false
	}
	for _, field := range fields.List {
		if expressionNamesType(field.Type, name) {
			return true
		}
	}
	return false
}

func expressionNamesType(expression ast.Expr, name string) bool {
	switch value := expression.(type) {
	case *ast.Ident:
		return value.Name == name
	case *ast.ParenExpr:
		return expressionNamesType(value.X, name)
	case *ast.StarExpr:
		return expressionNamesType(value.X, name)
	default:
		return false
	}
}

func fieldCount(fields *ast.FieldList) int {
	if fields == nil {
		return 0
	}
	count := 0
	for _, field := range fields.List {
		if len(field.Names) == 0 {
			count++
		} else {
			count += len(field.Names)
		}
	}
	return count
}
