// Copyright (c) 2026 Bohdan Futerko
// Website: https://www.bf.com.ua
// GitHub: https://github.com/LeBohdan

package engine

import (
	"encoding/json"
	"os"
	"testing"
)

func loadFixture(t *testing.T, name string) *Metrics {
	t.Helper()
	b, err := os.ReadFile("../../testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	var m Metrics
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("decode %s: %v", name, err)
	}
	return &m
}

func TestFixtureReading(t *testing.T) {
	m := loadFixture(t, "strata-metrics-reading.json")
	if m.Engine.Model != "qwen3.8-flash-next-iq3_s" {
		t.Errorf("unexpected engine model %q", m.Engine.Model)
	}
	if m.Live.State != "reading" {
		t.Errorf("expected state reading, got %s", m.Live.State)
	}
	if m.Live.PromptTokens != 12067 {
		t.Errorf("expected prompt_tokens 12067, got %d", m.Live.PromptTokens)
	}
	if m.Live.PromptRead != nil || m.Live.PromptTotal != nil {
		t.Errorf("expected null prompt progress, got %v/%v", m.Live.PromptRead, m.Live.PromptTotal)
	}
	if m.Hardware.GPUMemUsed != 25493045248 {
		t.Errorf("unexpected gpu_mem_used %d", m.Hardware.GPUMemUsed)
	}
}

func TestFixtureGenerating(t *testing.T) {
	m := loadFixture(t, "strata-metrics-generating.json")
	if m.Live.State != "generating" {
		t.Errorf("expected state generating, got %s", m.Live.State)
	}
	if m.Live.Generated != 12 {
		t.Errorf("expected generated 12, got %d", m.Live.Generated)
	}
	if m.Live.TokS == nil || *m.Live.TokS != 46.3 {
		t.Errorf("expected tok_s 46.3, got %v", m.Live.TokS)
	}
	if m.Live.PrefillTokSMean == nil || *m.Live.PrefillTokSMean != 1451.3 {
		t.Errorf("expected prefill_tok_s_mean 1451.3, got %v", m.Live.PrefillTokSMean)
	}
	if m.Hardware.GPUUtil == nil || *m.Hardware.GPUUtil != 100 {
		t.Errorf("expected gpu_util 100, got %v", m.Hardware.GPUUtil)
	}
}

func TestFixtureIdle(t *testing.T) {
	m := loadFixture(t, "strata-metrics-idle.json")
	if m.Live.State != "idle" {
		t.Errorf("expected state idle, got %s", m.Live.State)
	}
	if m.Hardware.RAMTotal <= 0 {
		t.Errorf("expected ram_total > 0, got %d", m.Hardware.RAMTotal)
	}
}
