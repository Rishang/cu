package ui

import (
	"bytes"
	"strings"
	"testing"
)

func TestColorJSONShape(t *testing.T) {
	withColor(t, true)

	var buf bytes.Buffer
	src := `{"a":{"b":[1,true,null,"x<y>"]},"empty":{},"none":[]}`
	if err := colorJSON(&buf, []byte(src)); err != nil {
		t.Fatal(err)
	}
	got := buf.String()

	// Stripping the escape codes must give back the same JSON, indented.
	plain := ansiPattern.ReplaceAllString(got, "")
	want := `{
  "a": {
    "b": [
      1,
      true,
      null,
      "x<y>"
    ]
  },
  "empty": {},
  "none": []
}
`
	if plain != want {
		t.Fatalf("colorJSON produced:\n%s\nwant:\n%s", plain, want)
	}
	// Keys and string values must be colored differently.
	if !strings.Contains(got, jsonKey.Render(`"a"`)) {
		t.Error("key not styled as a key")
	}
	if !strings.Contains(got, jsonStr.Render(`"x<y>"`)) {
		t.Error("string value not styled as a value")
	}
}
