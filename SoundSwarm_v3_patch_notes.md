# SoundSwarm v3 — Final Pre-Flight Patch Notes
## Five Claims Evaluated. Three Real Fixes. One Overcorrection. One Partial.

---

## Scoring Summary

| Issue | Real? | Severity | Fix Applied? |
|---|---|---|---|
| A. Asymmetric Jitter Buffer Deadlock | ✅ Real | **Critical** | ✅ Yes |
| B. Localhost Virtual Driver Circular Delay | ✅ Real | **High** | ✅ Yes |
| C. Hotspot Captive Portal / Cellular Fallback | ✅ Real | **High** | ✅ Yes (with nuance) |
| D. FFmpeg "overkill" — use raw PCM instead | ⚠️ Partially correct | Low | Modified |
| E. iOS Background UDP Throttling | ✅ Real, but fix is overcorrected | **High** | Modified |

---

## Fix A — Global Latency Equalization (Critical, Apply This)

### The Flaw
The adaptive jitter buffer in v3 lets each client independently size its buffer based on its own P95 jitter measurement. Phone A measures 140ms of jitter, sizes its buffer to 140ms, and plays audio 140ms late. Phone B measures 50ms, sizes to 50ms, plays audio 50ms late. They are 90ms out of sync with each other — the exact problem the entire sync engine was built to prevent.

### Why It's Real
This is a genuine architectural oversight. The per-client adaptive buffer was designed to handle network variance, but it creates a new source of inter-device offset if clients reach different values. The whole point of the timestamp-scheduled playback system is that every device plays the same absolute server timestamp at the same wall-clock moment. If their buffers are different depths, their "wall-clock moment" for any given timestamp diverges.

### The Fix
Clients do not autonomously set their active buffer depth. Instead:

1. Each client measures its local P95 jitter and reports it to the server over TCP every 5 seconds:
   ```
   CLIENT → SERVER (TCP): { "type": "JITTER_REPORT", "p95_ms": 87 }
   ```

2. The server collects all reports. It sets the global playback delay to:
   ```
   global_delay = max(all reported p95 values) + 20ms headroom
   ```
   Example: Phone A reports 87ms, Phone B reports 44ms, Phone C reports 61ms.
   Global delay = 87 + 20 = **107ms** for all devices.

3. The server broadcasts this to the entire swarm:
   ```
   SERVER → ALL CLIENTS (TCP): { "type": "SET_GLOBAL_LATENCY", "target_ms": 107 }
   ```

4. Every client pads its playback queue to match exactly 107ms of buffered audio ahead of its play cursor, regardless of its individual network quality. Phone B (which only needs 44ms) still buffers 107ms worth of audio — it just has extra runway. It never underruns.

5. This broadcast re-fires whenever any client's P95 changes by more than 10ms in either direction.

**Result:** All devices share the same playback timeline anchor. Per-client adaptive sizing is now only used to report upward to the server — it never directly controls local playback depth.

---

## Fix B — Laptop Client Loopback Offset (High, Apply This)

### The Flaw
The laptop server routes audio through a localhost socket into the same global jitter buffer (say 107ms). The phones play audio 107ms late. But the laptop's audio capture pipeline — WASAPI loopback on Windows, BlackHole on macOS — introduces its own capture latency *before* the audio even reaches the server. A typical WASAPI loopback adds ~10–20ms; BlackHole adds ~10ms. This capture latency is then *added* to the 107ms global buffer. The laptop's speakers play at ~127ms late, while phones play at 107ms late — a 20ms offset the user will hear.

### The Fix
The laptop client must apply a **static negative offset** equal to the measured loopback driver capture latency:

```
laptop_playback_target = global_latency_ms - loopback_capture_latency_ms
```

**Measuring the loopback latency:**  
During the acoustic calibration chirp pass, the laptop client participates as a listener too. The chirp is played out the laptop's own speakers. The WASAPI/BlackHole loopback captures it. The server timestamps when it "sent" the chirp into the pipeline, and when it "received" it back via the loopback. The delta is the capture roundtrip latency. Half of that is the one-way capture delay.

```
loopback_capture_latency = (loopback_received_timestamp - chirp_sent_timestamp) / 2
```

This is measured once at startup, stored, and subtracted from the laptop client's playback offset permanently. It does not need to be re-measured unless the audio driver changes.

**Practical expected values:**
- WASAPI loopback (Windows): typically 10–20ms
- BlackHole (macOS): typically 8–12ms  
- PipeWire ALSA loopback (Linux): typically 5–10ms

These values ensure the laptop's hardware output lands on the same global timeline as the phones.

---

## Fix C — Mobile Socket Binding on Hotspot Networks (High, Apply This — With Corrections)

### The Flaw
When the laptop creates a hotspot with no internet, iOS and Android both run captive portal detection: they attempt to reach `captive.apple.com` / `connectivitycheck.gstatic.com`. When these fail (no internet), both OSes present a notification asking the user to confirm staying connected, and may silently route app data traffic to cellular instead of WiFi.

### What the Reviewer Got Right
The cellular fallback risk is real. Both platforms can and do re-route socket traffic to cellular when WiFi has no internet. The explicit socket binding recommendation is correct in principle.

### What Needs Correction: The Implementation Details

**Android (correct as stated):**
```kotlin
// In the C++ JNI layer (via Kotlin wrapper):
val connectivityManager = context.getSystemService(ConnectivityManager::class.java)
val networkRequest = NetworkRequest.Builder()
    .addTransportType(NetworkCapabilities.TRANSPORT_WIFI)
    .build()

connectivityManager.requestNetwork(networkRequest, object : ConnectivityManager.NetworkCallback() {
    override fun onAvailable(network: Network) {
        // Bind the C++ layer's UDP and TCP sockets to this specific Network object
        network.bindSocket(udpSocket)  // DatagramSocket
        network.bindSocket(tcpSocket)  // Socket
    }
})
```
This is straightforward, works on API 21+, and is well-documented.

**iOS (more nuanced than the reviewer stated):**
The reviewer says to use `NWConnection` with `requiredInterfaceType = .wifi`. This is correct but incomplete — there's a problem. iOS 14+ disabled the VoIP socket keepalive capability for UDP (Apple removed it in iOS 4.1 for UDP, actually). The `NWConnection` approach works, but for the *audio data* path (UDP), you must use BSD sockets directly and bind to the WiFi interface address:

```c
// Get the WiFi interface address
struct ifaddrs *addrs;
getifaddrs(&addrs);
// Find the en0 (WiFi) interface
for (struct ifaddrs *addr = addrs; addr != NULL; addr = addr->ifa_next) {
    if (strcmp(addr->ifa_name, "en0") == 0 && addr->ifa_addr->sa_family == AF_INET) {
        // Bind UDP socket to this specific interface address
        bind(udpSocket, addr->ifa_addr, sizeof(struct sockaddr_in));
    }
}
```

For the TCP control channel, use `NWConnection` with `.requiredInterfaceType = .wifi` — this is cleaner and handles iOS's higher-level socket management properly.

**The captive portal notification itself:**
Add this to the laptop server's HTTP handler. When iOS/Android probe for internet connectivity, they send HTTP GET requests to known URLs (`captive.apple.com/hotspot-detect.html`, `connectivitycheck.gstatic.com/generate_204`). The SoundSwarm hotspot server can intercept these and respond correctly:

```go
// In the Go server's HTTP handler (runs on port 80 of the hotspot interface):
http.HandleFunc("/hotspot-detect.html", func(w http.ResponseWriter, r *http.Request) {
    // Apple expects this exact response to mark the network as "internet accessible"
    w.Header().Set("Content-Type", "text/html")
    fmt.Fprint(w, "<HTML><HEAD><TITLE>Success</TITLE></HEAD><BODY>Success</BODY></HTML>")
})

http.HandleFunc("/generate_204", func(w http.ResponseWriter, r *http.Request) {
    // Android expects HTTP 204 No Content
    w.WriteHeader(http.StatusNoContent)
})
```

This makes iOS and Android believe the hotspot has internet access. The captive portal notification disappears entirely. The OS keeps WiFi as the default route. No socket binding needed for most traffic — though explicit binding in the C++ audio layer is still good practice as defense-in-depth.

---

## Fix D — FFmpeg vs Raw PCM Demux (Partially Correct — Modified)

### The Reviewer's Claim
Using FFmpeg's libavcodec to demux 5.1/7.1 audio is "overkill." Instead, capture interleaved PCM from WASAPI/BlackHole and split channels with a simple array index loop.

### What's Right
For the basic surround sound use case — a movie playing through a normal media player where the OS mixer has already decoded the audio — the WASAPI loopback does indeed provide raw interleaved PCM. Splitting it is 6 lines of array indexing code. No FFmpeg needed for this path. This is a real simplification and should be adopted.

```go
// Splitting interleaved 5.1 PCM (float32) — in Go or C++
const channels = 6
for i := 0; i < len(pcmBuffer); i += channels {
    frontLeft[i/channels]  = pcmBuffer[i]
    frontRight[i/channels] = pcmBuffer[i+1]
    center[i/channels]     = pcmBuffer[i+2]
    lfe[i/channels]        = pcmBuffer[i+3]
    surroundLeft[i/channels]  = pcmBuffer[i+4]
    surroundRight[i/channels] = pcmBuffer[i+5]
}
```

### What's Wrong With Dropping FFmpeg Entirely
The reviewer misses a real use case: **direct file input mode**. If a user wants to stream from a video file directly (not through a system media player) — a common request for local MKV/MP4 files with embedded DTS or Dolby Digital tracks — the WASAPI loopback only sees the already-decoded output, not the raw surround channels if the OS mixer downmixed to stereo. In that scenario, FFmpeg is still needed to decode the raw container directly.

**Resolution:** Keep FFmpeg as an optional dependency for the direct-file streaming mode (Phase 4+). For Phase 1–3 (WASAPI/BlackHole loopback of system audio), use raw PCM channel splitting. No FFmpeg in the core server binary for the standard use case. This satisfies the reviewer's valid complaint about compilation complexity without cutting a genuinely needed feature.

---

## Fix E — iOS Background UDP Throttling (Real Problem, Wrong Fix)

### The Reviewer's Claim
When an iOS app backgrounds (screen locks), iOS coalesces UDP packet delivery — instead of 1 packet every 10ms, the app gets 0 packets for 40ms then a burst of 4. This overloads the jitter buffer. The fix: detect backgrounding via AppState, notify the server, and switch Opus frame size from 10ms to 40–60ms for that client.

### What's Right
The iOS background UDP coalescing behavior is real and documented. The symptom described is accurate.

### Why the Proposed Fix Is Incomplete
Switching Opus frame size to 60ms reduces packet rate from 100/sec to ~17/sec, which helps. But the reviewer misses the actual root cause and better primary solution.

**The real root cause:** iOS only keeps the network radio alive for background processes that declare the `audio` background mode. With `AVAudioSessionCategoryPlayback` configured and the app actually playing audio via AVAudioEngine, the audio background mode *keeps the app process alive* — but iOS still applies separate network power-saving to non-VoIP UDP sockets. The audio render thread stays alive; the UDP receive thread gets throttled.

**The actual fix (primary):**  
The audio rendering is happening in native C++ on a high-priority thread that writes samples to AVAudioEngine. That thread already has the data it needs (from the jitter buffer). The *receiver* thread reading UDP packets is a separate lower-priority thread that iOS throttles.

The solution is to use `NWConnection` (Apple's modern Network framework) instead of raw BSD UDP sockets, and configure it with:
```swift
let params = NWParameters.udp
params.serviceClass = .responsiveData  // Highest iOS network priority class
params.allowFastOpen = true
// This tells iOS scheduler this connection needs prompt delivery
```

`NWParameters.serviceClass = .responsiveData` is the correct API for telling iOS that this UDP stream is latency-sensitive and should not be coalesced. This is the iOS-native equivalent of DSCP QoS marking.

**The reviewer's fix (secondary, keep it):**  
The 40–60ms frame fallback is still a good *defense-in-depth* measure. If `serviceClass = .responsiveData` doesn't fully prevent coalescing on all iOS versions (behavior has changed across iOS releases), the larger frames are a graceful fallback. The trigger should be: server detects that a client's reported jitter has spiked above 30ms (indicating coalescing is occurring), then switches that client's stream to 40ms frames.

**The correct implementation order:**
1. Use `NWConnection` with `.serviceClass = .responsiveData` as the primary measure
2. Monitor per-client jitter on the server side
3. Automatically increase frame size to 40ms if jitter spikes, as a fallback
4. Restore 10ms frames when jitter normalizes (client moves screen back to foreground)

---

## Summary: What Actually Changes in v3 → v4

| Change | Type | Where |
|---|---|---|
| Global latency equalization: clients report P95 to server, server broadcasts single target | **New architecture** | C++ core (jitter reporter) + Go server |
| Laptop client applies negative offset = loopback capture latency | **New measurement** | Calibration pass + localhost client config |
| Captive portal spoofing: serve `/hotspot-detect.html` and `/generate_204` from Go server | **New HTTP handler** | Go server, hotspot mode only |
| Android socket binding: `network.bindSocket()` on WiFi Network object | **New code** | Android JNI wrapper |
| iOS socket binding: BSD `bind()` to en0 address for UDP; `NWConnection` + `.wifi` for TCP | **New code** | iOS AVAudioEngine wrapper |
| iOS UDP: use `NWParameters.serviceClass = .responsiveData` | **New API config** | iOS networking layer |
| iOS UDP fallback: server switches to 40ms frames on detected jitter spike | **New server logic** | Go server stream manager |
| Surround demux: PCM channel array split for loopback mode; FFmpeg for direct file mode only | **Simplified default path** | Go server + optional FFmpeg |
