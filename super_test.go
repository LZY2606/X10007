package jet

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func executeTemplate(t *testing.T, set *Set, name string, variables VarMap, context interface{}) (string, error) {
	t.Helper()
	tmpl, err := set.GetTemplate(name)
	if err != nil {
		t.Fatalf("getting template %q: %v", name, err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, variables, context); err != nil {
		return buf.String(), err
	}
	return buf.String(), nil
}

func TestSuperThreeLevelChain(t *testing.T) {
	l := NewInMemLoader()
	set := NewSet(l, WithSafeWriter(nil))
	l.Set("base.jet", `{{block body()}}base{{end}}`)
	l.Set("section.jet", `{{extends "base.jet"}}{{block body()}}section({{yield super()}}){{end}}`)
	l.Set("page.jet", `{{extends "section.jet"}}{{block body()}}page({{yield super()}}){{end}}`)

	out, err := executeTemplate(t, set, "page.jet", nil, nil)
	if err != nil {
		t.Fatalf("executing page.jet: %v", err)
	}
	if expected := "page(section(base))"; out != expected {
		t.Errorf("expected %q, got %q", expected, out)
	}

	// the intermediate templates still render their own chain
	out, err = executeTemplate(t, set, "section.jet", nil, nil)
	if err != nil {
		t.Fatalf("executing section.jet: %v", err)
	}
	if expected := "section(base)"; out != expected {
		t.Errorf("expected %q, got %q", expected, out)
	}
}

func TestSuperImportMergeOrder(t *testing.T) {
	l := NewInMemLoader()
	set := NewSet(l, WithSafeWriter(nil))
	l.Set("libA.jet", `{{block thing()}}A{{end}}`)
	l.Set("libB.jet", `{{block thing()}}B({{yield super()}}){{end}}`)
	// merge order in Set.parse: extends, then imports, then own blocks
	l.Set("main.jet", `{{extends "libA.jet"}}{{import "libB.jet"}}{{block thing()}}M({{yield super()}}){{end}}`)

	out, err := executeTemplate(t, set, "main.jet", nil, nil)
	if err != nil {
		t.Fatalf("executing main.jet: %v", err)
	}
	if expected := "M(B(A))"; out != expected {
		t.Errorf("expected %q, got %q", expected, out)
	}
}

func TestSuperParameters(t *testing.T) {
	l := NewInMemLoader()
	set := NewSet(l, WithSafeWriter(nil))
	l.Set("base.jet", `{{block greet(md=1, extra="E")}}base(md={{md}},extra={{extra}}){{end}}`)
	l.Set("section.jet", `{{extends "base.jet"}}{{block greet(md=2)}}section(md={{md}})[{{yield super()}}]{{end}}`)
	// explicit argument is passed down and then inherited further up the chain
	l.Set("page.jet", `{{extends "section.jet"}}{{block greet(md=3)}}page(md={{md}})[{{yield super(md=6)}}]{{end}}`)
	// no explicit argument: the current invocation's argument is inherited
	l.Set("page_inherit.jet", `{{extends "section.jet"}}{{block greet(md=3)}}page(md={{md}})[{{yield super()}}]{{end}}`)

	out, err := executeTemplate(t, set, "page.jet", nil, nil)
	if err != nil {
		t.Fatalf("executing page.jet: %v", err)
	}
	if expected := "page(md=3)[section(md=6)[base(md=6,extra=E)]]"; out != expected {
		t.Errorf("expected %q, got %q", expected, out)
	}

	out, err = executeTemplate(t, set, "page_inherit.jet", nil, nil)
	if err != nil {
		t.Fatalf("executing page_inherit.jet: %v", err)
	}
	if expected := "page(md=3)[section(md=3)[base(md=3,extra=E)]]"; out != expected {
		t.Errorf("expected %q, got %q", expected, out)
	}
}

func TestSuperContent(t *testing.T) {
	l := NewInMemLoader()
	set := NewSet(l, WithSafeWriter(nil))
	l.Set("lib.jet", `{{block wrap()}}<div>{{yield content}}</div>{{end}}`)
	// {{yield super()}} passes no content: the super definition's {{yield content}}
	// renders the content stored at the outermost yield call site
	l.Set("page.jet", `{{extends "lib.jet"}}{{block wrap()}}<section>{{yield super()}}</section>{{end}}`)
	l.Set("main.jet", `{{import "page.jet"}}{{yield wrap() content}}Hello {{name}}{{end}}`)
	// content can also be passed to super explicitly
	l.Set("page2.jet", `{{extends "lib.jet"}}{{block wrap()}}<section>{{yield super() content}}inner {{yield content}}{{end}}</section>{{end}}`)
	l.Set("main2.jet", `{{import "page2.jet"}}{{yield wrap() content}}X{{end}}`)

	vars := VarMap{}.Set("name", "Bob")
	out, err := executeTemplate(t, set, "main.jet", vars, nil)
	if err != nil {
		t.Fatalf("executing main.jet: %v", err)
	}
	if expected := "<section><div>Hello Bob</div></section>"; out != expected {
		t.Errorf("expected %q, got %q", expected, out)
	}

	out, err = executeTemplate(t, set, "main2.jet", nil, nil)
	if err != nil {
		t.Fatalf("executing main2.jet: %v", err)
	}
	if expected := "<section><div>inner X</div></section>"; out != expected {
		t.Errorf("expected %q, got %q", expected, out)
	}
}

func TestSuperWithoutPreviousDefinition(t *testing.T) {
	l := NewInMemLoader()
	set := NewSet(l, WithSafeWriter(nil))
	l.Set("nosuper.jet", "{{block solo()}}\n{{yield super()}}\n{{end}}{{yield solo()}}")

	_, err := executeTemplate(t, set, "nosuper.jet", nil, nil)
	if err == nil {
		t.Fatal("expected an error when yielding super() without a previous definition")
	}
	if !strings.Contains(err.Error(), `"/nosuper.jet":2`) {
		t.Errorf("expected error to mention template name and line, got: %v", err)
	}
	if !strings.Contains(err.Error(), "solo") {
		t.Errorf("expected error to mention the block name, got: %v", err)
	}
}

func TestSuperOutsideBlock(t *testing.T) {
	l := NewInMemLoader()
	set := NewSet(l, WithSafeWriter(nil))
	l.Set("toplevel.jet", `{{yield super()}}`)

	_, err := executeTemplate(t, set, "toplevel.jet", nil, nil)
	if err == nil {
		t.Fatal("expected an error when yielding super() outside of a block")
	}
	if !strings.Contains(err.Error(), `"/toplevel.jet":1`) {
		t.Errorf("expected error to mention template name and line, got: %v", err)
	}
}

func TestSuperScopeRestoration(t *testing.T) {
	l := NewInMemLoader()
	set := NewSet(l, WithSafeWriter(nil))
	set.AddGlobalFunc("boom", func(args Arguments) reflect.Value {
		panic(errors.New("boom"))
	})

	// variables declared inside the super definition must not leak into the caller
	l.Set("scopebase.jet", `{{block b()}}{{ v := "from-super" }}{{v}}{{end}}`)
	l.Set("scopepage.jet", `{{extends "scopebase.jet"}}{{block b()}}{{ v := "from-page" }}{{yield super()}}|{{v}}{{end}}`)

	out, err := executeTemplate(t, set, "scopepage.jet", nil, nil)
	if err != nil {
		t.Fatalf("executing scopepage.jet: %v", err)
	}
	if expected := "from-super|from-page"; out != expected {
		t.Errorf("expected %q, got %q", expected, out)
	}

	// scope must be restored when the super body returns early
	l.Set("retbase.jet", `{{block b()}}{{ v := "from-super" }}{{return "x"}}{{end}}`)
	l.Set("retpage.jet", `{{extends "retbase.jet"}}{{block b()}}{{ v := "from-page" }}{{yield super()}}|{{v}}{{end}}`)

	out, err = executeTemplate(t, set, "retpage.jet", nil, nil)
	if err != nil {
		t.Fatalf("executing retpage.jet: %v", err)
	}
	if expected := "|from-page"; out != expected {
		t.Errorf("expected %q, got %q", expected, out)
	}

	// scope must be restored when a panic inside the super body is caught by {{try}}
	l.Set("trybase.jet", `{{block b()}}{{ v := "from-super" }}{{boom()}}{{end}}`)
	l.Set("trypage.jet", `{{extends "trybase.jet"}}{{block b()}}{{ v := "from-page" }}{{try}}{{yield super()}}{{catch}}caught{{end}}|{{v}}{{end}}`)

	out, err = executeTemplate(t, set, "trypage.jet", nil, nil)
	if err != nil {
		t.Fatalf("executing trypage.jet: %v", err)
	}
	if expected := "caught|from-page"; out != expected {
		t.Errorf("expected %q, got %q", expected, out)
	}
}

func TestSuperAutoescape(t *testing.T) {
	l := NewInMemLoader()
	set := NewSet(l) // default set: HTML escaping on
	l.Set("escbase.jet", `{{block b()}}{{"<b>bold</b>"}}{{end}}`)
	l.Set("escpage.jet", `{{extends "escbase.jet"}}{{block b()}}[{{yield super()}}]{{end}}`)

	out, err := executeTemplate(t, set, "escpage.jet", nil, nil)
	if err != nil {
		t.Fatalf("executing escpage.jet: %v", err)
	}
	if expected := "[&lt;b&gt;bold&lt;/b&gt;]"; out != expected {
		t.Errorf("expected %q, got %q", expected, out)
	}
}

func TestSuperPrintSame(t *testing.T) {
	p := ParserTestCase{T: t}
	p.ExpectPrintSame(`{{block a(x=1)}}{{yield super()}}{{end}}`)
	p.ExpectPrintSame(`{{block a(x=1)}}{{yield super(x=2)}}{{end}}`)
	p.ExpectPrintSame(`{{block a()}}{{yield super() "ctx"}}{{end}}`)
	p.ExpectPrintSame(`{{block a()}}{{yield super() content}}x{{end}}{{end}}`)
	p.ExpectPrintSame(`{{block a()}}{{yield super(x=2) "ctx" content}}x{{end}}{{end}}`)
	p.TestPrintFile("super.jet")
}

func TestSuperConcurrentExecute(t *testing.T) {
	l := NewInMemLoader()
	set := NewSet(l, WithSafeWriter(nil))
	l.Set("cbase.jet", `{{block body()}}base{{end}}`)
	l.Set("cpage.jet", `{{extends "cbase.jet"}}{{block body()}}page({{yield super()}}){{end}}`)

	tmpl, err := set.GetTemplate("cpage.jet")
	if err != nil {
		t.Fatalf("getting template: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				var buf bytes.Buffer
				if err := tmpl.Execute(&buf, nil, nil); err != nil {
					t.Errorf("executing template: %v", err)
					return
				}
				if out := buf.String(); out != "page(base)" {
					t.Errorf("expected %q, got %q", "page(base)", out)
					return
				}
			}
		}()
	}
	wg.Wait()
}
