package backend

import (
	"bytes"
	"fmt"
	"github.com/jd3nn1s/gomidl/ast"
	"github.com/jd3nn1s/gomidl/scanner"
	"go/format"
	"log"
	"strings"
)

type Generator struct {
	packages map[string]struct{}
	buf      bytes.Buffer
}

var typeMap = map[string]string{
	"LONG":     "int32",
	"DWORD":    "uint32",
	"ULONG":    "uint32",
	"BYTE":     "byte",
	"LPWSTR":   "string",
	"LPCWSTR":  "string",
	"GUID":     "ole.GUID",
	"UINT_PTR": "*int32",
	"BOOL":     "bool",
	"REFGUID": "*ole.GUID",
	"HWND": "wintypes.HWND",
	"HBITMAP": "wintypes.HBITMAP",
	"REFPROPERTYKEY": "wintypes.PROPERTYKEY",
	"PROPVARIANT": "uintptr",
}

var olePackageStruct = map[string]struct{}{
	"IUnknown": {},
}

func (g *Generator) format() []byte {

	src, err := format.Source(g.buf.Bytes())
	if err != nil {
		log.Println("generated code will not format!:", err)
		log.Fatalln(string(g.buf.Bytes()))
	}
	return src
}

func (g *Generator) Printf(format string, args ...interface{}) {
	fmt.Fprintf(&g.buf, format, args...)
}

func (g *Generator) addPackage(pkg string) {
	if g.packages == nil {
		g.packages = make(map[string]struct{})
	}
	g.packages[pkg] = struct{}{}
}

func (g *Generator) genPackage(pkgName string) {
	buf := g.buf
	g.buf = bytes.Buffer{}

	g.Printf("package %s\n\n", pkgName)
	g.Printf("import (\n")
	g.Printf("\"golang.org/x/text/encoding/unicode\"\n")
	for k := range g.packages {
		g.Printf("%s\n", k)
	}
	g.Printf(")\n\n")
	g.Printf("var utf16Encoder = unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM).NewEncoder()\n")
	g.Printf("\n")
	buf.WriteTo(&g.buf)
}

func (g *Generator) genEnum(n *ast.EnumNode) {
	g.Printf("type %s int\n", n.Name)
	g.Printf("const (\n")
	for i, v := range n.Values {
		enumVal := v.Val
		if (enumVal == "" || enumVal == "0") && i == 0 {
			enumVal = "iota"
		}
		if enumVal == "" {
			g.Printf("%s\n", v.Name)
		} else {
			g.Printf("%s %s = %s\n", v.Name, n.Name, enumVal)
		}
	}
	g.Printf(")\n")
}

func (g *Generator) genStruct(n *ast.StructNode) {
	g.Printf("type %s struct {\n", n.Name)
	for _, f := range n.Fields {
		g.Printf("%s %s\n", f.Name, g.mapType(f.Type))
	}
	g.Printf("}\n")
}

func (g *Generator) genTypedef(n *ast.TypedefNode) {
	t := g.mapType(n.Type)
	if t == "void" {
		t = "struct{}"
	}
	g.Printf("type %s %s\n", n.Name, t)
}

func (g *Generator) genInterface(n *ast.InterfaceNode) {
	if len(n.Methods) == 0 {
		// skip interface definitions that have no methods
		return
	}

	g.Printf("/////////////////////////////////////////\n")
	g.Printf("// %s\n", n.Name)
	// generate vtable
	g.Printf("\ntype %sVtbl struct {\n", n.Name)
	pn := n.ParentName
	if pn == "" {
		pn = "IUnknown"
	}

	if _, ok := olePackageStruct[pn]; ok {
		pn = "ole." + pn
		g.addPackage("ole \"github.com/go-ole/go-ole\"")
	}

	g.Printf("%sVtbl\n", pn)

	for _, m := range n.Methods {
		g.Printf("%s uintptr\n", m.Name)
	}
	g.Printf("}\n\n")

	g.Printf("type %s struct {\n", n.Name)
	g.Printf("%s\n", pn)
	g.Printf("}\n\n")

	g.Printf("func (v *%s) VTable() *%sVtbl {\n", n.Name, n.Name)
	g.Printf("return (*%sVtbl)(unsafe.Pointer(v.RawVTable))\n", n.Name)
	g.Printf("}\n\n")
	g.addPackage("\"unsafe\"")

	for _, m := range n.Methods {
		g.Printf("func (v *%s) %s(\n", n.Name, m.Name)

		syscallParams := make([]string, 0, len(m.Params))
		for _, n := range m.Params {
			param, comment := g.genParam(n)
			g.Printf("%s, // %s\n", param, comment)
		}
		g.Printf(") error {\n")

		for _, n := range m.Params {
			syscallParam, convertCode := g.genSyscallParams(n)
			syscallParams = append(syscallParams, syscallParam...)
			g.Printf("%s\n", convertCode)
		}
			syscallFunc := "Syscall"
		numSyscallRequired := 3
		numSyscallParams := len(m.Params) + 1
		if numSyscallParams <= 3 {
		} else if numSyscallParams <= 6 {
			syscallFunc = "Syscall6"
			numSyscallRequired = 6
		} else if numSyscallParams <= 9 {
			syscallFunc = "Syscall9"
			numSyscallRequired = 9
		}

		for i := len(syscallParams); i < numSyscallRequired-1; i++ {
			syscallParams = append(syscallParams, "0")
		}

		g.Printf("hr, _, _ := syscall.%s(\n", syscallFunc)
		g.Printf("v.VTable().%s,\n", m.Name)
		g.Printf("%d,\n", len(syscallParams)+1)
		g.Printf("uintptr(unsafe.Pointer(v)),\n")

		for _, param := range syscallParams {
			g.Printf("%s,\n", param)
		}

		g.Printf(")\n")
		g.Printf("if hr != 0 { return ole.NewError(hr) }\n")
		g.Printf("return nil")
		g.Printf("}\n")
		g.addPackage("\"syscall\"")
	}

}

func (g *Generator) mapType(t string) string {
	if v, ok := typeMap[t]; ok {
		t = v
	}

	if strings.HasPrefix(t, "wintypes.") {
		g.addPackage("\"github.com/jd3nn1s/gomidl/wintypes\"")
	} else if strings.HasPrefix(t, "ole.") {
		g.addPackage("ole \"github.com/go-ole/go-ole\"")
	}
	return t
}

func (g *Generator) genParam(n *ast.ParamNode) (string, string) {
	ptrs := ""
	for i := 0; i < n.Indirections; i++ {
		ptrs += "*"
	}
	param := fmt.Sprintf("%s %s%s",
		n.Name,
		ptrs,
		g.mapType(n.Type))

	attrs := make([]string, 0, len(n.Attributes))
	for _, a := range n.Attributes {
		c := ""
		switch a.Type {
		case scanner.IN:
			c = "in"
		case scanner.OUT:
			c = "out"
		}
		if c != "" {
			if a.Val != "" {
				c += fmt.Sprintf("(%s)", a.Val)
			}
			attrs = append(attrs, c)
		}
	}
	return param, fmt.Sprintf("[%s]", strings.Join(attrs, ", "))
}

func (g *Generator) genSyscallParams(param *ast.ParamNode) ([]string, string) {
	goType := g.mapType(param.Type)
	indirections := param.Indirections + strings.Count(goType, "*")
	if goType == "VARIANT" && indirections == 0{
		prefix := param.Name
		return []string{prefix+"V0", prefix+"V1", prefix+"V2"}, ""
	} else if goType == "bool" && indirections == 0 {
		return []string{fmt.Sprintf("uintptr(%s_bool)", param.Name)},
			fmt.Sprintf("var %s_bool int\n" +
				"if %s == true { %s_bool = 1 }", param.Name,param.Name,param.Name)
	} else if indirections > 0 {
		return []string{fmt.Sprintf("uintptr(unsafe.Pointer(%s))", param.Name)}, ""
	} else if goType == "string" {
		switch v := param.Type; {
		case v == "LPWSTR":
			fallthrough
		case v == "LPCWSTR":
			g.addPackage("\"log\"")
			return []string{fmt.Sprintf("uintptr(unsafe.Pointer(&%s_rawval[0]))", param.Name)},
				fmt.Sprintf("%s_rawval, %s_err := utf16Encoder.Bytes([]byte(%s))\n"+
					"if %s_err != nil { log.Fatalln(\"unable to utf-16 encode:\", %s_err) }\n", param.Name, param.Name, param.Name, param.Name, param.Name)
		default:
			log.Fatalln("cannot convert string to original type:", v)
		}
	}
	return []string{fmt.Sprintf("uintptr(%s)", param.Name)}, ""
}
