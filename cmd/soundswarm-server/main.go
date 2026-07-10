package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/soundswarm/soundswarm/internal/capture"
	"github.com/soundswarm/soundswarm/internal/codec"
	"github.com/soundswarm/soundswarm/internal/discovery"
	"github.com/soundswarm/soundswarm/internal/protocol"
	"github.com/soundswarm/soundswarm/internal/server"
	"github.com/soundswarm/soundswarm/internal/session"
	"github.com/soundswarm/soundswarm/internal/stream"
	"github.com/soundswarm/soundswarm/internal/surround"
	ssync "github.com/soundswarm/soundswarm/internal/sync"
)

func main() {
	// 1. Parse configuration
	port := flag.Int("port", 8080, "HTTP server port (TCP will be port+1)")
	udpPort := flag.Int("udp-port", 8082, "UDP streaming port")
	hotspot := flag.Bool("hotspot", false, "Enable captive portal spoofing for hotspot mode")
	debug := flag.Bool("debug", false, "Enable debug logging")
	testTone := flag.Bool("test-tone", false, "Generate a 440Hz test tone instead of capturing Windows audio")
	flag.Parse()

	// 2. Setup logging
	logLevel := slog.LevelInfo
	if *debug {
		logLevel = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel}))
	slog.SetDefault(logger)

	logger.Info("Starting SoundSwarm v4 Server",
		"http_port", *port,
		"tcp_port", *port+1,
		"udp_port", *udpPort,
		"hotspot_mode", *hotspot,
	)

	// 3. Initialize core services
	sessMgr := session.NewManager(logger.With("module", "session"))
	defer sessMgr.Stop()

	sess, err := sessMgr.CreateSession()
	if err != nil {
		logger.Error("Failed to create session", "error", err)
		os.Exit(1)
	}

	streamMgr, err := stream.NewManager(*udpPort, logger.With("module", "stream"))
	if err != nil {
		logger.Error("Failed to start stream manager", "error", err)
		os.Exit(1)
	}
	defer streamMgr.Stop()

	clockSync := ssync.NewClockSync(logger.With("module", "clock"))

	// FrameSizeSetter callback for Fix E (iOS background fallback)
	setFrameSize := func(clientID string, frameMs int) {
		streamMgr.SetFrameSize(clientID, frameMs)
		sessClient := sess.GetClient(clientID)
		if sessClient != nil {
			sessClient.SetFrameSize(frameMs)
		}
		// Send SET_FRAME_SIZE to client via TCP
		// In a full implementation, we'd route this via the TCPServer, but for decoupling
		// we let the TCP client read loop handle frame size changes if needed, or we broadcast it.
	}

	var tcpServer *server.TCPServer // Declaration for broadcaster closure

	latencyEq := ssync.NewLatencyEqualizer(
		func(delayMs float64) {
			// Broadcaster callback: send SET_GLOBAL_LATENCY to all clients
			msg := protocol.SetGlobalLatencyMsg{
				Type:     protocol.MsgSetGlobalLatency,
				TargetMs: delayMs,
			}
			if tcpServer != nil {
				tcpServer.BroadcastToAll(msg)
			}
		},
		setFrameSize,
		logger.With("module", "latency"),
	)

	qrGen := discovery.NewQRGenerator(logger.With("module", "qr"))

	// 4. Initialize servers
	httpConfig := server.HTTPConfig{
		Port:           *port,
		UDPPort:        *udpPort,
		HotspotMode:    *hotspot,
		SessionManager: sessMgr,
		StreamManager:  streamMgr,
		LatencyEq:      latencyEq,
		QRGenerator:    qrGen,
		Logger:         logger.With("module", "http"),
	}
	httpSrv := server.NewHTTPServer(httpConfig)

	tcpConfig := server.TCPConfig{
		Port:           *port + 1,
		SessionManager: sessMgr,
		StreamManager:  streamMgr,
		ClockSync:      clockSync,
		LatencyEq:      latencyEq,
		OnClientJoin:   func(id string) { httpSrv.BroadcastUpdate() },
		OnClientLeave:  func(id string) { httpSrv.BroadcastUpdate() },
		OnUIUpdate:     func() { httpSrv.BroadcastUpdate() },
		Logger:         logger.With("module", "tcp"),
	}
	tcpServer = server.NewTCPServer(tcpConfig)

	// 5. Initialize audio pipeline
	var cap capture.AudioCapture
	if *testTone {
		logger.Info("Using 440Hz test tone generator instead of WASAPI capture")
		cap = capture.NewTestToneCapture(48000, 2)
	} else {
		cap = capture.NewCapture()
	}

	if err := cap.Start(); err != nil {
		logger.Error("Failed to start audio capture", "error", err)
		os.Exit(1)
	}
	defer cap.Stop()

	// Fix B: Laptop Loopback Offset
	latencyEq.SetLoopbackCompensation("laptop_host", cap.LoopbackLatencyMs())

	format := cap.Format()
	logger.Info("Audio capture started",
		"sample_rate", format.SampleRate,
		"channels", format.Channels,
		"latency_ms", cap.LoopbackLatencyMs(),
	)

	// Initialize encoders and demuxer
	var demuxer *surround.Demuxer
	var encoders []*codec.Encoder
	
	layout, err := surround.LayoutForChannels(format.Channels)
	if err != nil {
		logger.Error("Unsupported channel layout", "error", err)
		os.Exit(1)
	}
	
	if format.Channels > 2 {
		demuxer = surround.NewDemuxer(layout)
		logger.Info("Surround demuxer initialized", "layout", layout.Name)
	}

	// Create one encoder per mono channel (or 1 for stereo interleaved)
	encConfig := codec.DefaultEncoderConfig()
	encConfig.SampleRate = format.SampleRate
	
	if demuxer != nil {
		encConfig.Channels = 1
		for i := 0; i < format.Channels; i++ {
			enc, err := codec.NewEncoder(encConfig)
			if err != nil {
				logger.Error("Failed to create encoder", "error", err)
				os.Exit(1)
			}
			encoders = append(encoders, enc)
		}
	} else {
		encConfig.Channels = format.Channels
		enc, err := codec.NewEncoder(encConfig)
		if err != nil {
			logger.Error("Failed to create encoder", "error", err)
			os.Exit(1)
		}
		encoders = append(encoders, enc)
	}

	// 6. Start mDNS and discovery
	mdns, err := discovery.NewMDNSServer(*port+1, sess.ID, logger.With("module", "mdns"))
	if err != nil {
		logger.Warn("Failed to start mDNS", "error", err)
	} else {
		defer mdns.Stop()
	}

	// 7. Start servers
	if err := httpSrv.Start(); err != nil {
		logger.Error("HTTP server failed", "error", err)
		os.Exit(1)
	}
	defer httpSrv.Stop()

	if err := tcpServer.Start(); err != nil {
		logger.Error("TCP server failed", "error", err)
		os.Exit(1)
	}
	defer tcpServer.Stop()

	streamMgr.StartReceiver()
	sessMgr.StartHeartbeatMonitor()

	// 8. Main audio loop
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	go runAudioPipeline(ctx, cap, demuxer, encoders, streamMgr, logger.With("module", "pipeline"))

	// 9. Graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	
	logger.Info("Server running. Press Ctrl+C to stop.", "session_id", sess.ID)
	
	<-sigChan
	logger.Info("Shutting down...")
}

func runAudioPipeline(ctx context.Context, cap capture.AudioCapture, demuxer *surround.Demuxer, encoders []*codec.Encoder, streamMgr *stream.Manager, logger *slog.Logger) {
	// Hot path allocations
	format := cap.Format()
	frameSamples := encoders[0].FrameSamples()
	interleavedBuf := make([]float32, frameSamples*format.Channels)

	var demuxBufs [][]float32
	var stereoBuf []float32
	if demuxer != nil {
		demuxBufs = make([][]float32, format.Channels)
		for i := 0; i < format.Channels; i++ {
			demuxBufs[i] = make([]float32, frameSamples)
		}
		stereoBuf = make([]float32, frameSamples*2)
	}

	// F1 fix: Audio time must be mathematical, not systemic.
	// If we use ServerTimeNow() inside the loop, GC pauses cause the timestamp
	// to jump forward artificially. The C++ clients interpret this as network jitter.
	//
	// Instead, we anchor the pipeline to a single wall-clock reference taken
	// BEFORE the loop starts, and advance the timestamp mathematically based
	// on the number of samples we have actually processed. GC pauses do not
	// affect this counter.
	var totalSamplesProcessed int64
	pipelineStartUs := ssync.ServerTimeNow()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// 1. Read audio
		n, err := cap.Read(interleavedBuf)
		if err != nil {
			logger.Error("Capture read error", "error", err)
			time.Sleep(10 * time.Millisecond) // backoff
			continue
		}
		if n == 0 {
			logger.Info("debug: cap.Read returned 0")
			continue
		}

		// F1 fix: Compute the capture timestamp from sample count, not wall clock.
		captureTs := pipelineStartUs + (totalSamplesProcessed*1_000_000)/int64(format.SampleRate)
		
		// Soft-sync check: handle hardware clock drift and WASAPI silence gaps.
		nowUs := ssync.ServerTimeNow()
		deltaUs := nowUs - captureTs
		
		if deltaUs > 100000 || deltaUs < -100000 { 
			// >100ms drift means a huge gap (e.g. WASAPI paused during silence). Snap instantly.
			logger.Warn("Large audio gap detected, snapping timestamp", "delta_us", deltaUs)
			pipelineStartUs += deltaUs
			captureTs += deltaUs
		} else if deltaUs > 5000 {
			// Slew forward by 20us per frame (4ms/sec correction rate)
			pipelineStartUs += 20
		} else if deltaUs < -5000 {
			// Slew backward by 20us per frame
			pipelineStartUs -= 20
		}
		
		totalSamplesProcessed += int64(n / format.Channels)

		// 2. Demux and Encode
		if demuxer != nil {
			// Surround mode: split channels and encode independently
			err := demuxer.DemuxInto(interleavedBuf, demuxBufs)
			if err != nil {
				logger.Error("Demux error", "error", err)
				continue
			}

			layout := demuxer.Layout()
			for chIdx, buf := range demuxBufs {
				encoded, err := encoders[chIdx].Encode(buf)
				if err != nil {
					logger.Error("Encode error", "channel", chIdx, "error", err)
					continue
				}
				channelMask := layout.Mapping[chIdx]
				streamMgr.SendAudio(encoded, channelMask, captureTs, protocol.CodecPCM)
				codec.ReturnEncoderBuffer(encoded)
			}
			
			// Generate stereo downmix for clients not assigned a specific surround channel
			err = surround.DownmixToStereoInto(demuxBufs, layout, stereoBuf)
			if err == nil {
				_ = stereoBuf
			}

		} else {
			// Stereo/Mono mode: encode interleaved directly
			logger.Debug("Encoding stereo/mono...")
			encoded, err := encoders[0].Encode(interleavedBuf)
			if err != nil {
				logger.Error("Encode error", "error", err)
				continue
			}
			streamMgr.SendAudio(encoded, protocol.ChannelStereoMix, captureTs, protocol.CodecPCM)
			codec.ReturnEncoderBuffer(encoded)
		}
	}
}
