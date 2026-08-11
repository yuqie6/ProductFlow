package question

import (
	"encoding/json"
	"testing"
)

func TestNormalizeAndAnswerRoundTrip(t *testing.T) {
	prompt, err := Normalize("", " choose ", []Option{{Label: " A "}, {Label: "B", Description: " second "}})
	if err != nil {
		t.Fatal(err)
	}
	if prompt.Header != "需要确认" || prompt.Question != "choose" || prompt.Options[0].Label != "A" || prompt.Options[1].Description != "second" {
		t.Fatalf("prompt = %#v", prompt)
	}
	raw, err := EncodeAnswer(prompt, 1)
	if err != nil {
		t.Fatal(err)
	}
	answer, err := DecodeAnswer(raw, prompt)
	if err != nil || answer.Option != 1 || answer.Label != "B" {
		t.Fatalf("answer = %#v, err = %v", answer, err)
	}
}

func TestFreeTextAnswerRoundTrip(t *testing.T) {
	prompt, err := Normalize("", "choose", []Option{{Label: "A"}})
	if err != nil {
		t.Fatal(err)
	}
	answer, err := NewFreeTextAnswer("  use a custom scope  ")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(answer)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeAnswer(raw, prompt)
	if err != nil || decoded.Option != FreeTextOption || decoded.Label != FreeTextLabel || decoded.Text != "use a custom scope" {
		t.Fatalf("answer = %#v, err = %v", decoded, err)
	}
	if _, err := NewFreeTextAnswer(" \n "); err == nil {
		t.Fatal("empty free-text answer accepted")
	}
}

func TestQuestionContractRejectsAmbiguousOrForgedAnswers(t *testing.T) {
	if _, err := Normalize("", "choose", []Option{{Label: "same"}, {Label: " same "}}); err == nil {
		t.Fatal("duplicate options accepted")
	}
	prompt, err := Normalize("", "choose", []Option{{Label: "A"}, {Label: "B"}})
	if err != nil {
		t.Fatal(err)
	}
	forged, _ := json.Marshal(Answer{Option: 0, Label: "B"})
	if _, err := DecodeAnswer(forged, prompt); err == nil {
		t.Fatal("forged option label accepted")
	}
	forged, _ = json.Marshal(Answer{Option: FreeTextOption, Label: "A", Text: "custom"})
	if _, err := DecodeAnswer(forged, prompt); err == nil {
		t.Fatal("forged free-text label accepted")
	}
}
