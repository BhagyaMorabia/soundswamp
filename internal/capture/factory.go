// Package capture — factory function.
package capture

// NewCapture returns the platform-specific AudioCapture implementation.
// On Windows, this returns a WASAPICapture.
// On macOS, this returns a CoreAudioCapture.
// On other platforms, this returns a StubCapture (sine wave generator).
func NewCapture() AudioCapture {
	return platformNewCapture()
}
