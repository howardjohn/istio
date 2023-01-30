package main

import (
	_ "embed"
	"fmt"
	"go/ast"
	"go/parser"
	"runtime"
	"strings"
	"unsafe"
)

//go:embed main.go
var fileContents string
var Map5x = mapper(fileContents)

func mapper(fc string) func() {
	lines := strings.Split(fc, "\n")
	f, err := parser.ParseExpr(fc)
	_ = err
	fmt.Println(f)
	fmt.Println(err)

	return func() {
		_, _, l, _ := runtime.Caller(2)
		fmt.Println("line ", l, lines[l-1])

		f, err := parser.ParseExpr(lines[l-1])
		_ = err
		fmt.Println(f)
		fmt.Printf("%T\n", f)
		fmt.Println(err)
		fn := f.(*ast.CallExpr).Fun.(*ast.SelectorExpr).X.(*ast.Ident).Name
		fmt.Println(fn)
		// If fn is Map5 ...
		t := f.(*ast.CallExpr).Args[0].(*ast.CompositeLit).Type.(*ast.Ident)
		fmt.Println(t.Name)
	}
}

type Person struct {
	First, Last string
}

var MapPerson = Person{}

func main() {
	arr := []Person{
		{First: "John", Last: "Doe"},
		{First: "John", Last: "Smith"},
	}

	fmt.Println(Map(arr, func(t Person) string {
		return t.Last
	}))

	fmt.Println(Map2(arr, &arr[0].Last))
	fmt.Println(Map3(arr, Ptr(Person{}.Last)))
	fmt.Println(Map4[Person, string](arr, unsafe.Offsetof(Person{}.Last)))
	fmt.Println(Map5(arr, Person{}.Last))
}

func Map[T, U any](data []T, f func(T) U) []U {
	res := make([]U, 0, len(data))
	for _, e := range data {
		res = append(res, f(e))
	}
	return res
}

func Map2[T, U any](data []T, ptr *U) []U {
	base := uintptr(unsafe.Pointer(&data[0]))
	offset := uintptr(unsafe.Pointer(ptr))
	fmt.Println("Offset", offset-base)
	return Map(data, func(t T) U {
		return *((*U)(unsafe.Add(unsafe.Pointer(&t), offset-base)))
	})
}

func Map3[T, U any](data []T, ptr *U) []U {
	return nil
}

func Map4[T, U any](data []T, offset uintptr) []U {
	return Map(data, func(t T) U {
		return *((*U)(unsafe.Add(unsafe.Pointer(&t), offset)))
	})
}

func Map5[T, U any](data []T, offset U) []U {
	Map5x()
	return nil
	//return Map(data, func(t T) U {
	//	return *((*U)(unsafe.Add(unsafe.Pointer(&t), offset)))
	//})
}

func Ptr[T any](data T) *T {
	return &data
}
