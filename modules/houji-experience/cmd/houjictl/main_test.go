package main

import "testing"

func TestRequestDecoderRejectsTrailingAndUnknownData(t *testing.T) {
	for _, payload := range []string{`{"action":"status","command":"reboot"}`, `{"action":"status"}{"action":"enable"}`, `{"action":"ai_save","ai":{"unknown":true}}`, `[]`} {
		if _, e := parseRequest([]byte(payload)); e == nil {
			t.Fatal("accepted invalid request", payload)
		}
	}
	if r, e := parseRequest([]byte(`{"action":"ai_ask","prompt":"$(reboot) <script>"}`)); e != nil || r.Prompt != "$(reboot) <script>" {
		t.Fatal(r, e)
	}
}
