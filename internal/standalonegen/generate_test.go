package standalonegen_test

import (
	"bytes"
	"go/parser"
	"go/token"
	"testing"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/internal/standalonegen"
)

func TestGenerateNativeInitializer(t *testing.T) {
	source := []byte{0, 97, 115, 109, 1, 0, 0, 0}
	c, err := wago.Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	generated, err := standalonegen.Generate(c, "StandaloneCompiled")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parser.ParseFile(token.NewFileSet(), "native.go", generated, parser.AllErrors); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(generated, []byte("func StandaloneCompiled() *Compiled")) {
		t.Fatal("missing native factory")
	}
	for _, forbidden := range []string{"sourceCompilation", "MarshalBinary", "UnmarshalBinary", "base:", "execution:", "once:"} {
		if bytes.Contains(generated, []byte(forbidden)) {
			t.Errorf("initializer contains process/build state: %s", forbidden)
		}
	}
}

func TestGenerateRejectsInvalidInput(t *testing.T) {
	for _, input := range []any{nil, 42, (*wago.Compiled)(nil)} {
		if _, err := standalonegen.Generate(input, "StandaloneCompiled"); err == nil {
			t.Fatalf("accepted %T", input)
		}
	}
	c, err := wago.Compile([]byte{0, 97, 115, 109, 1, 0, 0, 0})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := standalonegen.Generate(c, "x() {}"); err == nil {
		t.Fatal("accepted an invalid factory identifier")
	}
}
