package tutor

import "testing"

func TestParseChallengePlain(t *testing.T) {
	raw := `{"id":"sum","prompt":"add","starter_code":"def add(a,b):\n  pass","tests":["assert add(1,2)==3"]}`
	ch, err := parseChallenge(raw)
	if err != nil {
		t.Fatal(err)
	}
	if ch.ID != "sum" || len(ch.Tests) != 1 {
		t.Fatalf("unexpected: %+v", ch)
	}
}

func TestParseChallengeFenced(t *testing.T) {
	raw := "Here you go:\n```json\n{\"id\":\"x\",\"prompt\":\"p\",\"starter_code\":\"s\",\"tests\":[\"assert True\"]}\n```\n"
	ch, err := parseChallenge(raw)
	if err != nil {
		t.Fatal(err)
	}
	if ch.ID != "x" || ch.Prompt != "p" {
		t.Fatalf("unexpected: %+v", ch)
	}
}
