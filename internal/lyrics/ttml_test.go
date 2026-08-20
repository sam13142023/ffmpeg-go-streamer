package lyrics

import "testing"

func TestParseTTMLSyllables(t *testing.T) {
	raw := `<?xml version="1.0"?><tt xml:lang="en"><body><div>
<p begin="00:00:01.000" end="00:00:03.000"><span begin="1s" end="2s">Hello</span> <span begin="2s" end="3s">world</span></p>
<p begin="00:00:04.500" end="00:00:06.000">Next line</p>
</div></body></tt>`
	got, err := ParseTTML(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Synced || got.Language != "en" || len(got.Lines) != 2 {
		t.Fatalf("unexpected lyrics: %#v", got)
	}
	if got.Lines[0].Text != "Hello world" || got.Lines[0].StartMS != 1000 || len(got.Lines[0].Words) != 2 {
		t.Fatalf("unexpected first line: %#v", got.Lines[0])
	}
	if active := got.ActiveLineAt(4700); active != 1 {
		t.Fatalf("active line = %d, want 1", active)
	}
}

func TestParseTTMLEmpty(t *testing.T) {
	got, err := ParseTTML("  ")
	if err != nil || !got.Empty() {
		t.Fatalf("got %#v, %v", got, err)
	}
}
