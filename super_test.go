package jet

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
)

// countingLoader is an in-memory Loader that counts Open() calls per path.
type countingLoader struct {
	mu    sync.Mutex
	files map[string]string
	opens map[string]int
}

func newCountingLoader() *countingLoader {
	return &countingLoader{files: map[string]string{}, opens: map[string]int{}}
}

func (l *countingLoader) Set(templatePath, contents string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.files[templatePath] = contents
}

func (l *countingLoader) Exists(templatePath string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	_, ok := l.files[templatePath]
	return ok
}

func (l *countingLoader) Open(templatePath string) (io.ReadCloser, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	content, ok := l.files[templatePath]
	if !ok {
		return nil, fmt.Errorf("%s does not exist", templatePath)
	}
	l.opens[templatePath]++
	return io.NopCloser(strings.NewReader(content)), nil
}

func (l *countingLoader) openCount(templatePath string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.opens[templatePath]
}

func renderTemplate(t *testing.T, set *Set, name string) string {
	t.Helper()
	tt, err := set.GetTemplate(name)
	if err != nil {
		t.Fatalf("GetTemplate(%q): %v", name, err)
	}
	var buf bytes.Buffer
	if err := tt.Execute(&buf, nil, nil); err != nil {
		t.Fatalf("Execute(%q): %v", name, err)
	}
	return buf.String()
}

func TestSuperThreeLevelChain(t *testing.T) {
	l := NewInMemLoader()
	set := NewSet(l, WithSafeWriter(nil))
	l.Set("base.jet", `{{block body()}}base{{end}}`)
	l.Set("section.jet", `{{extends "base.jet"}}{{block body()}}section({{yield super()}}){{end}}`)
	l.Set("page.jet", `{{extends "section.jet"}}{{block body()}}page({{yield super()}}){{end}}`)

	if got, want := renderTemplate(t, set, "page.jet"), "page(section(base))"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	// rendering the middle template directly must use its own chain
	if got, want := renderTemplate(t, set, "section.jet"), "section(base)"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSuperImportChain(t *testing.T) {
	l := NewInMemLoader()
	set := NewSet(l, WithSafeWriter(nil))
	l.Set("lib.jet", `{{block hello() "Buddy"}}Hello {{.}}{{end}}`)
	l.Set("page.jet", `{{import "lib.jet"}}{{block hello() "Buddy"}}Hey {{.}} / {{yield super()}}{{end}}`)

	if got, want := renderTemplate(t, set, "page.jet"), "Hey Buddy / Hello Buddy"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSuperParameters(t *testing.T) {
	l := NewInMemLoader()
	set := NewSet(l, WithSafeWriter(nil))
	l.Set("base.jet", `{{block greet(md=1)}}base-md={{md}}{{end}}`)
	l.Set("section.jet", `{{extends "base.jet"}}{{block greet(md=2)}}section-md={{md}}|{{yield super()}}{{end}}`)
	l.Set("page_explicit.jet", `{{extends "section.jet"}}{{block greet(md=3)}}page-md={{md}}|{{yield super(md=6)}}{{end}}`)
	l.Set("page_inherit.jet", `{{extends "section.jet"}}{{block greet(md=3)}}page-md={{md}}|{{yield super()}}{{end}}`)

	// explicit parameter is passed to the super definition and inherited further down the chain
	if got, want := renderTemplate(t, set, "page_explicit.jet"), "page-md=3|section-md=6|base-md=6"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	// without explicit parameters the currently bound arguments are inherited
	if got, want := renderTemplate(t, set, "page_inherit.jet"), "page-md=3|section-md=3|base-md=3"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSuperParameterDefaultFallback(t *testing.T) {
	l := NewInMemLoader()
	set := NewSet(l, WithSafeWriter(nil))
	// the overriding definition does not bind md, so the super definition's
	// own default value must be used
	l.Set("base.jet", `{{block greet(md=7)}}base-md={{md}}{{end}}`)
	l.Set("page.jet", `{{extends "base.jet"}}{{block greet()}}{{yield super()}}{{end}}`)

	if got, want := renderTemplate(t, set, "page.jet"), "base-md=7"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSuperContent(t *testing.T) {
	l := NewInMemLoader()
	set := NewSet(l, WithSafeWriter(nil))
	l.Set("base.jet", `{{block wrap()}}<base>{{yield content}}</base>{{end}}`)
	l.Set("section.jet", `{{extends "base.jet"}}{{block wrap()}}<section>{{yield super()}}</section>{{end}}`)
	l.Set("page.jet", `{{extends "section.jet"}}{{block wrap()}}<page>{{yield super() content}}PAGE-CONTENT{{end}}</page>{{end}}`)

	// {{yield content}} in the base definition renders the content passed at
	// the outermost call site
	if got, want := renderTemplate(t, set, "page.jet"), "<page><section><base>PAGE-CONTENT</base></section></page>"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSuperNoSuperError(t *testing.T) {
	l := NewInMemLoader()
	set := NewSet(l, WithSafeWriter(nil))
	l.Set("lone.jet", `{{block lone()}}{{yield super()}}{{end}}`)

	tt, err := set.GetTemplate("lone.jet")
	if err != nil {
		t.Fatalf("GetTemplate: %v", err)
	}
	var buf bytes.Buffer
	err = tt.Execute(&buf, nil, nil)
	if err == nil {
		t.Fatal("expected error, got none")
	}
	if !strings.Contains(err.Error(), `"/lone.jet":1`) {
		t.Errorf("error %q does not contain template name and line", err.Error())
	}
	if !strings.Contains(err.Error(), "no super definition") {
		t.Errorf("unexpected error message: %q", err.Error())
	}
}

func TestSuperScopeRestored(t *testing.T) {
	l := NewInMemLoader()
	set := NewSet(l, WithSafeWriter(nil))
	// variables declared inside the super definition's body must not leak
	// into the calling scope
	l.Set("base.jet", `{{block b(p="base")}}{{ v := "super-var" }}{{v}}{{end}}`)
	l.Set("page.jet", `{{extends "base.jet"}}{{block b(p="page")}}{{ v := "page-var" }}{{yield super()}}|{{v}}|{{p}}{{end}}`)

	if got, want := renderTemplate(t, set, "page.jet"), "super-var|page-var|page"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSuperScopeRestoredAfterTryCatch(t *testing.T) {
	l := NewInMemLoader()
	set := NewSet(l, WithSafeWriter(nil))
	l.Set("base.jet", `{{block b(p="base-default")}}{{ nosuchvar }}{{end}}`)
	l.Set("page.jet", `{{extends "base.jet"}}{{block b(p="page")}}{{try}}{{yield super(p="super")}}{{catch}}caught:{{end}}{{p}}{{end}}`)

	// the scope pushed for the super call (with p="super") must be released
	// even though the super body panicked and was caught by try
	if got, want := renderTemplate(t, set, "page.jet"), "caught:page"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSuperReturnInsideSuperBody(t *testing.T) {
	l := NewInMemLoader()
	set := NewSet(l, WithSafeWriter(nil))
	l.Set("base.jet", `{{block b(p="base")}}{{return "r"}}{{end}}`)
	l.Set("page.jet", `{{extends "base.jet"}}{{block b(p="page")}}{{yield super()}}done:{{p}}{{end}}`)

	if got, want := renderTemplate(t, set, "page.jet"), "done:page"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSuperAutoescape(t *testing.T) {
	l := NewInMemLoader()
	set := NewSet(l) // default set escapes HTML
	l.Set("base.jet", `{{block b()}}{{"<h1>base</h1>"}}{{end}}`)
	l.Set("page.jet", `{{extends "base.jet"}}{{block b()}}{{yield super()}}{{end}}`)

	if got, want := renderTemplate(t, set, "page.jet"), "&lt;h1&gt;base&lt;/h1&gt;"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSuperParsePrintSame(t *testing.T) {
	p := ParserTestCase{T: t}
	p.ExpectPrintSame(`{{block a()}}{{yield super()}}{{end}}`)
	p.ExpectPrintSame(`{{yield super(md=6)}}`)
	p.ExpectPrintSame(`{{yield super() content}}x{{end}}`)
	p.ExpectPrintSame(`{{yield super(md=6) "ctx" content}}x{{end}}`)
}

func TestReloadTransitiveInvalidation(t *testing.T) {
	l := newCountingLoader()
	l.Set("/base.jet", `{{block body()}}base-v1{{end}}`)
	l.Set("/mid.jet", `{{extends "base.jet"}}{{block body()}}mid({{yield super()}}){{end}}`)
	l.Set("/page.jet", `{{extends "mid.jet"}}{{block body()}}page({{yield super()}}){{end}}`)
	l.Set("/other.jet", `other-v1`)
	set := NewSet(l, WithSafeWriter(nil))

	if got, want := renderTemplate(t, set, "page.jet"), "page(mid(base-v1))"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got, want := renderTemplate(t, set, "other.jet"), "other-v1"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}

	l.Set("/base.jet", `{{block body()}}base-v2{{end}}`)
	l.Set("/other.jet", `other-v2`)

	if err := set.Reload("base.jet"); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	// page and mid transitively depend on base and must be re-parsed
	if got, want := renderTemplate(t, set, "page.jet"), "page(mid(base-v2))"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	// other does not depend on base: it must be served from the cache
	// without asking the loader again
	if got, want := renderTemplate(t, set, "other.jet"), "other-v1"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	for path, want := range map[string]int{
		"/base.jet":  2,
		"/mid.jet":   2,
		"/page.jet":  2,
		"/other.jet": 1,
	} {
		if got := l.openCount(path); got != want {
			t.Errorf("Open(%q) called %d times, want %d", path, got, want)
		}
	}
}

func TestReloadOnlyDependents(t *testing.T) {
	l := newCountingLoader()
	l.Set("/base.jet", `{{block body()}}base-v1{{end}}`)
	l.Set("/page.jet", `{{extends "base.jet"}}{{block body()}}page({{yield super()}}){{end}}`)
	set := NewSet(l, WithSafeWriter(nil))

	renderTemplate(t, set, "page.jet")

	l.Set("/page.jet", `{{extends "base.jet"}}{{block body()}}page-v2({{yield super()}}){{end}}`)
	if err := set.Reload("page.jet"); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	if got, want := renderTemplate(t, set, "page.jet"), "page-v2(base-v1)"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	// base is a dependency of the reloaded template, not a dependent:
	// it must not be fetched again
	if got := l.openCount("/base.jet"); got != 1 {
		t.Errorf("Open(/base.jet) called %d times, want 1", got)
	}
}

func TestReloadUnknownTemplate(t *testing.T) {
	l := newCountingLoader()
	l.Set("/base.jet", `base`)
	set := NewSet(l, WithSafeWriter(nil))
	if err := set.Reload("does-not-exist"); err == nil {
		t.Error("expected error reloading unknown template, got nil")
	}
}

func TestSuperConcurrentExecute(t *testing.T) {
	l := NewInMemLoader()
	set := NewSet(l, WithSafeWriter(nil))
	l.Set("base.jet", `{{block body()}}base{{end}}`)
	l.Set("mid.jet", `{{extends "base.jet"}}{{block body()}}mid({{yield super()}}){{end}}`)
	l.Set("page.jet", `{{extends "mid.jet"}}{{block body()}}page({{yield super()}}){{end}}`)

	tt, err := set.GetTemplate("page.jet")
	if err != nil {
		t.Fatalf("GetTemplate: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				var buf bytes.Buffer
				if err := tt.Execute(&buf, nil, nil); err != nil {
					t.Error(err)
					return
				}
				if got, want := buf.String(), "page(mid(base))"; got != want {
					t.Errorf("got %q, want %q", got, want)
					return
				}
			}
		}()
	}
	wg.Wait()
}

func TestReloadConcurrentWithExecute(t *testing.T) {
	l := newCountingLoader()
	l.Set("/base.jet", `{{block body()}}base-v1{{end}}`)
	l.Set("/page.jet", `{{extends "base.jet"}}{{block body()}}page({{yield super()}}){{end}}`)
	set := NewSet(l, WithSafeWriter(nil))

	renderTemplate(t, set, "page.jet")

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// one goroutine keeps reloading base.jet
	var reloader sync.WaitGroup
	reloader.Add(1)
	go func() {
		defer reloader.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			l.Set("/base.jet", fmt.Sprintf(`{{block body()}}base-v%d{{end}}`, i%2+1))
			if err := set.Reload("base.jet"); err != nil {
				t.Error(err)
				return
			}
		}
	}()

	// other goroutines keep fetching and executing; every single execution
	// must render a consistent snapshot
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				tt, err := set.GetTemplate("page.jet")
				if err != nil {
					t.Error(err)
					return
				}
				var buf bytes.Buffer
				if err := tt.Execute(&buf, nil, nil); err != nil {
					t.Error(err)
					return
				}
				switch got := buf.String(); got {
				case "page(base-v1)", "page(base-v2)":
				default:
					t.Errorf("inconsistent render output: %q", got)
					return
				}
			}
		}()
	}

	wg.Wait()
	close(stop)
	reloader.Wait()
}
