// Copyright (c) 2026 Bohdan Futerko
// Website: https://www.bf.com.ua
// GitHub: https://github.com/LeBohdan

package models

import "time"

type MetricEvent struct {
	Type string `json:"type"`

	Slot int `json:"slot"`
	Task int `json:"task"`

	Phase string `json:"phase,omitempty"`

	PromptTokens      int     `json:"prompt_tokens,omitempty"`
	PromptTPS         float64 `json:"prompt_tps,omitempty"`
	PromptEvalMs      float64 `json:"prompt_eval_ms,omitempty"`
	Progress          float64 `json:"progress,omitempty"`

	Keep              float64 `json:"keep,omitempty"`
	GeneratedTokens   int     `json:"generated_tokens,omitempty"`
	GenerationTPS     float64 `json:"generation_tps,omitempty"`
	GenerationTPS3s   float64 `json:"generation_tps_3s,omitempty"`

	TotalMs      float64 `json:"total_ms,omitempty"`
	TotalTokens   int     `json:"total_tokens,omitempty"`

	GraphsReused int `json:"graphs_reused,omitempty"`
	Truncated    int `json:"truncated,omitempty"`
	NTokens      int `json:"n_tokens,omitempty"`

	Timestamp time.Time `json:"timestamp"`
}
