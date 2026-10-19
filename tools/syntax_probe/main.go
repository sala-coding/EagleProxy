package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
)

func main() {
	fs := token.NewFileSet()
	for _, path := range os.Args[1:] {
		f, err := parser.ParseFile(fs, path, nil, 0)
		if err != nil {
			fmt.Println("parse err:", path, err)
			continue
		}
		fmt.Println("==", path)
		for _, d := range f.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok {
				fmt.Println("  func", fn.Name.Name, "line", fs.Position(fn.Pos()).Line)
			}
		}
	}
}