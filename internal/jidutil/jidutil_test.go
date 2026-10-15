package jidutil

import "testing"

func TestParse(t *testing.T) {
	cases := []struct {
		in, want string
		wantErr  bool
	}{
		{"+1 555 0100", "15550100@s.whatsapp.net", false},
		{"1 (555) 010-0100", "15550100100@s.whatsapp.net", false},
		{"15550100", "15550100@s.whatsapp.net", false},
		{"123456789@s.whatsapp.net", "123456789@s.whatsapp.net", false},
		{"123456789:12@s.whatsapp.net", "123456789@s.whatsapp.net", false},
		{"123456789012345@lid", "123456789012345@lid", false},
		{"123456789-123@g.us", "123456789-123@g.us", false},
		{"status@broadcast", "status@broadcast", false},
		{"", "", true},
		{"abc", "", true},
		{"12", "", true},
		{"@s.whatsapp.net", "", true},
	}
	for _, c := range cases {
		got, err := Parse(c.in)
		if (err != nil) != c.wantErr {
			t.Errorf("Parse(%q) err = %v, wantErr %v", c.in, err, c.wantErr)
			continue
		}
		if err == nil && got.String() != c.want {
			t.Errorf("Parse(%q) = %s, want %s", c.in, got, c.want)
		}
	}
}

func TestFromStoredSender(t *testing.T) {
	cases := map[string]string{
		"15550100":                  "15550100@s.whatsapp.net",
		"123456789012345":           "123456789012345@lid", // >13 digits: LID
		"123456789@lid":             "123456789@lid",
		"15550100:3@s.whatsapp.net": "15550100@s.whatsapp.net",
	}
	for in, want := range cases {
		got, ok := FromStoredSender(in)
		if !ok || got.String() != want {
			t.Errorf("FromStoredSender(%q) = %s, %v; want %s", in, got, ok, want)
		}
	}
	if _, ok := FromStoredSender(""); ok {
		t.Error("empty sender should not parse")
	}
}

func TestHelpers(t *testing.T) {
	if !IsGroup("123-456@g.us") || IsGroup("15550100@s.whatsapp.net") {
		t.Error("IsGroup")
	}
	if UserPart("15550100:2@s.whatsapp.net") != "15550100" {
		t.Error("UserPart")
	}
	if DigitsOnly("+1 (555) 0100") != "15550100" {
		t.Error("DigitsOnly")
	}
}
