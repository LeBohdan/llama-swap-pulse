// Copyright (c) 2026 Bohdan Futerko
// Website: https://www.bf.com.ua
// GitHub: https://github.com/LeBohdan

package models

import "time"

// Upstream engine kinds recognized by the poller.
const (
	EngineStrata   = "strata"
	EngineMaya     = "maya"
	EngineLlamaCpp = "llamacpp"
	EngineUnknown  = "unknown"
)

// Upstream state values (shared by Strata and Maya live snapshots).
const (
	StateIdle       = "idle"
	StateReading    = "reading"
	StateGenerating = "generating"
	StateQueued     = "queued"
	StateUnloaded   = "unloaded"
)

// GPUStats holds GPU telemetry reported by an engine.
type GPUStats struct {
	Util     *float64 `json:"util,omitempty"`
	MemUsed  int64    `json:"mem_used,omitempty"`
	MemTotal int64    `json:"mem_total,omitempty"`
	Temp     *float64 `json:"temp,omitempty"`
	Power    *float64 `json:"power,omitempty"`
}

// RAMStats holds host memory telemetry reported by an engine.
type RAMStats struct {
	Used  int64 `json:"used,omitempty"`
	Total int64 `json:"total,omitempty"`
}

// UpstreamState is the current status of a single llama-swap upstream
// (engine kind, activity state, queue depth and hardware telemetry).
type UpstreamState struct {
	Upstream string `json:"upstream"`
	Engine   string `json:"engine"`

	State  string `json:"state"`
	Phase  string `json:"phase,omitempty"`
	Queued int    `json:"queued"`

	Model string `json:"model,omitempty"`

	GPU *GPUStats `json:"gpu,omitempty"`
	RAM *RAMStats `json:"ram,omitempty"`
	CPU float64   `json:"cpu,omitempty"`

	UpdatedAt time.Time `json:"updated_at"`
}
