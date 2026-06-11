package tutor

import "testing"

func TestParseNoteContent(t *testing.T) {
	raw := "```json\n" +
		`{"title":"Derivatives","subject":"math","tags":["calculus"],"body":"A derivative measures change. See [[Limits]]."}` +
		"\n```"
	nc, err := parseNoteContent(raw)
	if err != nil {
		t.Fatal(err)
	}
	if nc.Title != "Derivatives" || nc.Subject != "math" {
		t.Fatalf("metadata: %+v", nc)
	}
	if len(nc.Tags) != 1 || nc.Tags[0] != "calculus" {
		t.Fatalf("tags: %v", nc.Tags)
	}
	if nc.Body == "" {
		t.Fatalf("body lost")
	}
}

func TestExtractJSONObject(t *testing.T) {
	if s, ok := extractJSONObject("noise {\"a\":1} trailing"); !ok || s != `{"a":1}` {
		t.Fatalf("plain: %q ok=%v", s, ok)
	}
	if _, ok := extractJSONObject("no object here"); ok {
		t.Fatalf("should not find an object")
	}
}

func TestOfflineNote(t *testing.T) {
	nc := offlineNote("the cold war")
	if nc.Title != "the cold war" || nc.Body == "" {
		t.Fatalf("offline note: %+v", nc)
	}
}
