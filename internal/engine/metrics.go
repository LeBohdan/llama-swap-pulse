// Copyright (c) 2026 Bohdan Futerko
// Website: https://www.bf.com.ua
// GitHub: https://github.com/LeBohdan

package engine

// Metrics is the JSON payload served by a Strata/Maya engine at GET /metrics
// (both engines expose an identical engine/live/hardware shape; extra keys
// such as the request history are ignored).
type Metrics struct {
	Engine   EngineInfo `json:"engine"`
	Live     Live       `json:"live"`
	Hardware Hardware   `json:"hardware"`
}

type EngineInfo struct {
	Model   string `json:"model"`
	Version string `json:"version"`
	Engine  string `json:"engine"`
}

// Live is the current activity snapshot of one engine.
type Live struct {
	State  string `json:"state"`
	Queued int    `json:"queued"`
	Phase  string `json:"phase"`

	PromptTokens int  `json:"prompt_tokens"`
	PromptRead   *int `json:"prompt_read"`
	PromptTotal  *int `json:"prompt_total"`

	Generated int `json:"generated"`
	MaxTokens int `json:"max_tokens"`

	ElapsedS float64 `json:"elapsed_s"`

	TokS            *float64 `json:"tok_s"`
	TokSMean        *float64 `json:"tok_s_mean"`
	PrefillTokSMean *float64 `json:"prefill_tok_s_mean"`
	TokSWindowS     *float64 `json:"tok_s_window_s"`
}

// Hardware is the engine-reported host telemetry. Byte counters and
// percentages; fields the engine does not report stay nil.
type Hardware struct {
	GPUUtil     *float64 `json:"gpu_util"`
	GPUMemUsed  int64    `json:"gpu_mem_used"`
	GPUMemTotal int64    `json:"gpu_mem_total"`
	GPUTemp     *float64 `json:"gpu_temp"`
	GPUPower    *float64 `json:"gpu_power"`

	CPU float64 `json:"cpu"`

	RAMUsed  int64 `json:"ram_used"`
	RAMTotal int64 `json:"ram_total"`
}
