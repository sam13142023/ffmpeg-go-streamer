package lyrics

import "testing"

func TestParseLRC(t *testing.T) {
	got, err := ParseLRC("[ar:Coldplay]\n[00:01.20]Look at the stars\n[00:03.450][00:05.00]Look how they shine", 8000)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Synced || len(got.Lines) != 3 {
		t.Fatalf("unexpected lyrics %#v", got)
	}
	if got.Lines[0].StartMS != 1200 || got.Lines[0].EndMS != 3450 {
		t.Fatalf("unexpected first timing %#v", got.Lines[0])
	}
	if got.Lines[2].StartMS != 5000 || got.Lines[2].EndMS != 8000 {
		t.Fatalf("unexpected last timing %#v", got.Lines[2])
	}
}

func TestParseLRCRejectsPlainText(t *testing.T) {
	if _, err := ParseLRC("no timestamps here", 0); err == nil {
		t.Fatal("expected error")
	}
}
