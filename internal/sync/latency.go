// Package sync — Global Latency Equalization (Fix A + Fix B from v3 patch notes).
//
// Fix A (Critical): Clients do NOT autonomously set their jitter buffer depth.
// Instead, each client reports its P95 jitter to the server every 5 seconds.
// The server computes a single global playback delay:
//
//	global_delay = max(all_client_p95_values) + headroom
//
// This value is broadcast to every client via SET_GLOBAL_LATENCY. Every client
// buffers exactly this amount of audio ahead of its play cursor, ensuring all
// devices share the same playback timeline anchor.
//
// Fix B (High): The laptop client (which receives its own audio through the
// loopback capture pipeline) subtracts the measured loopback capture latency
// from the global delay:
//
//	laptop_playback_target = global_delay - loopback_capture_latency
//
// This compensates for the WASAPI/BlackHole/PipeWire capture delay that the
// phone clients don't experience.
package sync

import (
	"log/slog"
	"math"
	gosync "sync"
	"time"
)

const (
	// DefaultHeadroomMs is added to the max P95 jitter for safety margin.
	DefaultHeadroomMs = 20.0

	// MinGlobalDelayMs prevents the global delay from being unreasonably small.
	MinGlobalDelayMs = 30.0

	// MaxGlobalDelayMs caps the global delay to prevent excessive latency.
	MaxGlobalDelayMs = 500.0

	// JitterReportInterval is how often clients send P95 jitter reports.
	JitterReportInterval = 5 * time.Second

	// ReBroadcastThresholdMs triggers a new SET_GLOBAL_LATENCY when any
	// client's P95 changes by this amount.
	ReBroadcastThresholdMs = 10.0

	// IosBgJitterThresholdMs triggers frame size increase for iOS background mode.
	IosBgJitterThresholdMs = 30.0

	// IosBgFrameMs is the fallback Opus frame size for backgrounded iOS clients.
	IosBgFrameMs = 40
)

// LatencyBroadcaster is called when the global delay changes.
// The implementation sends SET_GLOBAL_LATENCY to all connected clients.
type LatencyBroadcaster func(delayMs float64)

// FrameSizeSetter is called when a specific client's Opus frame size should change.
// Used for iOS background throttle mitigation (Fix E fallback).
type FrameSizeSetter func(clientID string, frameMs int)

// LatencyEqualizer computes and broadcasts the global playback delay.
type LatencyEqualizer struct {
	mu          gosync.RWMutex
	reports     map[string]*jitterReport // clientID → latest report
	globalDelay float64                   // current global delay in ms
	headroom    float64                   // headroom added to max P95
	broadcaster LatencyBroadcaster
	frameSetter FrameSizeSetter
	logger      *slog.Logger

	// Laptop loopback compensation (Fix B)
	laptopClientID      string
	loopbackLatencyMs   float64
}

type jitterReport struct {
	P95Ms     float64
	UpdatedAt time.Time
}

// NewLatencyEqualizer creates a new latency equalizer.
func NewLatencyEqualizer(broadcaster LatencyBroadcaster, frameSetter FrameSizeSetter, logger *slog.Logger) *LatencyEqualizer {
	return &LatencyEqualizer{
		reports:     make(map[string]*jitterReport),
		headroom:    DefaultHeadroomMs,
		globalDelay: MinGlobalDelayMs,
		broadcaster: broadcaster,
		frameSetter: frameSetter,
		logger:      logger,
	}
}

// SetLoopbackCompensation configures the laptop client's loopback offset (Fix B).
// This must be called after the WASAPI/CoreAudio capture measures its pipeline latency.
func (le *LatencyEqualizer) SetLoopbackCompensation(laptopClientID string, captureLatencyMs float64) {
	le.mu.Lock()
	defer le.mu.Unlock()
	le.laptopClientID = laptopClientID
	le.loopbackLatencyMs = captureLatencyMs
	le.logger.Info("loopback compensation set",
		"laptop_client", laptopClientID,
		"capture_latency_ms", captureLatencyMs,
	)
}

// ReportJitter processes a jitter report from a client and recalculates the
// global delay if necessary. This is the main entry point called when a
// JITTER_REPORT TCP message arrives.
func (le *LatencyEqualizer) ReportJitter(clientID string, p95Ms float64) {
	le.mu.Lock()
	defer le.mu.Unlock()

	old, exists := le.reports[clientID]
	le.reports[clientID] = &jitterReport{
		P95Ms:     p95Ms,
		UpdatedAt: time.Now(),
	}

	// Check if this change is significant enough to warrant re-broadcast
	if exists && math.Abs(p95Ms-old.P95Ms) < ReBroadcastThresholdMs {
		return // change too small, no re-broadcast
	}

	le.recalculate()

	// iOS background throttle detection (Fix E fallback):
	// If a client's P95 spikes above 30ms, it's likely backgrounded on iOS
	// and experiencing UDP coalescing. Switch its stream to 40ms frames.
	if p95Ms > IosBgJitterThresholdMs && le.frameSetter != nil {
		le.frameSetter(clientID, IosBgFrameMs)
		le.logger.Info("iOS background detected, switching to larger frames",
			"client", clientID,
			"frame_ms", IosBgFrameMs,
			"jitter_ms", p95Ms,
		)
	} else if exists && old.P95Ms > IosBgJitterThresholdMs && p95Ms <= IosBgJitterThresholdMs {
		// Jitter normalized — restore normal frame size
		le.frameSetter(clientID, 10)
		le.logger.Info("iOS foreground restored, switching to normal frames",
			"client", clientID,
			"frame_ms", 10,
		)
	}
}

// recalculate computes the new global delay from all client reports.
// Caller must hold le.mu.
func (le *LatencyEqualizer) recalculate() {
	if len(le.reports) == 0 {
		return
	}

	// Find the maximum P95 across all clients
	var maxP95 float64
	for _, report := range le.reports {
		if report.P95Ms > maxP95 {
			maxP95 = report.P95Ms
		}
	}

	newDelay := maxP95 + le.headroom

	// Clamp to valid range
	if newDelay < MinGlobalDelayMs {
		newDelay = MinGlobalDelayMs
	}
	if newDelay > MaxGlobalDelayMs {
		newDelay = MaxGlobalDelayMs
	}

	// Only broadcast if the delay actually changed significantly
	if math.Abs(newDelay-le.globalDelay) < 1.0 {
		return
	}

	oldDelay := le.globalDelay
	le.globalDelay = newDelay

	le.logger.Info("global latency updated",
		"old_ms", oldDelay,
		"new_ms", newDelay,
		"max_p95_ms", maxP95,
		"headroom_ms", le.headroom,
		"client_count", len(le.reports),
	)

	// Broadcast to all clients
	if le.broadcaster != nil {
		le.broadcaster(newDelay)
	}
}

// RemoveClient removes a client's jitter report and recalculates.
func (le *LatencyEqualizer) RemoveClient(clientID string) {
	le.mu.Lock()
	defer le.mu.Unlock()

	delete(le.reports, clientID)
	le.recalculate()
}

// GlobalDelay returns the current global delay in milliseconds.
func (le *LatencyEqualizer) GlobalDelay() float64 {
	le.mu.RLock()
	defer le.mu.RUnlock()
	return le.globalDelay
}

// LaptopPlaybackTarget returns the effective playback target for the laptop client,
// accounting for the loopback capture latency (Fix B).
func (le *LatencyEqualizer) LaptopPlaybackTarget() float64 {
	le.mu.RLock()
	defer le.mu.RUnlock()

	target := le.globalDelay - le.loopbackLatencyMs
	if target < 0 {
		target = 0
	}
	return target
}

// GetClientReport returns the latest P95 jitter for a client.
func (le *LatencyEqualizer) GetClientReport(clientID string) (float64, bool) {
	le.mu.RLock()
	defer le.mu.RUnlock()

	report, exists := le.reports[clientID]
	if !exists {
		return 0, false
	}
	return report.P95Ms, true
}

// SetHeadroom updates the headroom added to the max P95 jitter.
func (le *LatencyEqualizer) SetHeadroom(ms float64) {
	le.mu.Lock()
	defer le.mu.Unlock()
	le.headroom = ms
	le.recalculate()
}
