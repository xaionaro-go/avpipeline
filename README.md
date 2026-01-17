# About

[![License: CC0-1.0](https://img.shields.io/badge/License-CC0%201.0-lightgrey.svg)](http://creativecommons.org/publicdomain/zero/1.0/)

This package is targeted on unlocking [`libav`](https://en.wikipedia.org/wiki/Libav) capabilities to build dynamic pipelines for processing audio/video.

Core concepts:
- **Kernel**: Implements specific media processing logic (I/O, transcoding, filtering).
- **Processor**: Manages a Kernel, providing queuing, synchronization, and observability.
- **Node**: A vertex in the pipeline graph, handling data routing and lifecycle.

For example:
```go
	ctx, cancelFn := context.WithCancel(ctx)
	defer cancelFn()

	// 1. Initialize an input (e.g., from an RTMP URL)
	inputKernel, err := kernel.NewInputFromURL(ctx, fromURL, secret.New(""), kernel.InputConfig{})
	assert(ctx, err == nil, err)
	defer inputKernel.Close(ctx)
	inputNode := node.NewFromKernel(ctx, inputKernel, processor.DefaultOptionsInput()...)

	// 2. Initialize a transcoder (e.g., to H.264 using NVIDIA hardware acceleration)
	hwDevName := codec.HardwareDeviceName("cuda")
	transcoderKernel, err := kernel.NewTranscoder(
		ctx,
		codec.NewNaiveDecoderFactory(ctx, &codec.NaiveDecoderFactoryParams{
			HardwareDeviceName: hwDevName,
		}),
		codec.NewNaiveEncoderFactory(ctx, &codec.NaiveEncoderFactoryParams{
			VideoCodec:         codec.Name("h264_nvenc"),
			AudioCodec:         codec.NameCopy, // Passthrough audio
			HardwareDeviceName: hwDevName,
		}),
		nil, // Default encoder config
	)
	assert(ctx, err == nil, err)
	defer transcoderKernel.Close(ctx)
	transcoderNode := node.NewFromKernel(ctx, transcoderKernel, processor.DefaultOptionsTranscoder()...)

	// 3. Initialize an output (e.g., to an RTMP URL)
	outputKernel, err := kernel.NewOutputFromURL(ctx, toURL, secret.New(""), kernel.OutputConfig{})
	assert(ctx, err == nil, err)
	defer outputKernel.Close(ctx)
	outputNode := node.NewFromKernel(ctx, outputKernel, processor.DefaultOptionsOutput()...)

	// 4. Connect nodes: input -> transcoder -> output
	inputNode.AddPushTo(ctx, transcoderNode)
	transcoderNode.AddPushTo(ctx, outputNode)

	// 5. Start the pipeline
	errCh := make(chan node.Error, 100)
	observability.Go(ctx, func(ctx context.Context) {
		avpipeline.Serve(ctx, avpipeline.ServeConfig{
			EachNode: node.ServeConfig{
				FrameDropVideo: true,
			},
		}, errCh, inputNode)
	})

	// 6. Monitor for errors and statistics
	statusTicker := time.NewTicker(time.Second)
	defer statusTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case err, ok := <-errCh:
			if !ok {
				return
			}
			switch {
			case errors.Is(err.Err, context.Canceled), errors.Is(err.Err, io.EOF):
				continue
			default:
				logger.Errorf(ctx, "pipeline error: %v", err)
			}
		case <-statusTicker.C:
			inputStats := inputNode.GetCountersPtr()
			outputStats := outputNode.GetCountersPtr()
			fmt.Printf("Input: %d packets | Output: %d packets\n",
				inputStats.Sent.Packets.TotalCount(),
				outputStats.Received.Packets.TotalCount(),
			)
		}
	}
```

The provided example could be represented this way:
```mermaid
graph LR
    SRC[Source: e.g. Network/Disk] -- "Receive" --> IK

    subgraph IN [Input Node]
        direction TB
        IK[Kernel: Input] -- "1. Generate()" --> IP[Processor]
        IP -- "2. OutputChan()" --> IS["Node's Serve() loop"]
    end

    subgraph TN [Transcoder Node]
        direction TB
        TP[Processor] -- "4. SendInput()" --> TK[Kernel: Transcoder]
        TK -- "5. OutputChan" --> TP
        TP -- "6. OutputChan()" --> TS["Node's Serve() loop"]
    end

    subgraph ON [Output Node]
        direction TB
        OP[Processor] -- "8. SendInput()" --> OK[Kernel: Output]
    end

    IS -- "3. Push (to InputChan)" --> TP
    TS -- "7. Push (to InputChan)" --> OP

    OK -- "Send" --> SNK[Sink: e.g. Network/Disk]

    style SRC fill:#333,stroke:#666,color:#fff
    style SNK fill:#333,stroke:#666,color:#fff
    style IN fill:#333,stroke:#666,color:#fff
    style TN fill:#333,stroke:#666,color:#fff
    style ON fill:#333,stroke:#666,color:#fff
    style IP fill:#000066,stroke:#0055ff,color:#fff
    style TP fill:#000066,stroke:#0055ff,color:#fff
    style OP fill:#000066,stroke:#0055ff,color:#fff
    style IK fill:#663300,stroke:#ff6600,color:#fff
    style TK fill:#663300,stroke:#ff6600,color:#fff
    style OK fill:#663300,stroke:#ff6600,color:#fff
    style IS fill:#003300,stroke:#008000,color:#fff
    style TS fill:#003300,stroke:#008000,color:#fff
```

# Examples

* [`avd`](https://github.com/xaionaro-go/avd) users `avpipeline` to implement a streaming server (as an alternative to [`mediamtx`](https://github.com/bluenviron/mediamtx)).
* [`ffstream`](https://github.com/xaionaro-go/ffstream) uses `avpipeline` to implement a CLI that could be used as a kick-in replacement to `ffmpeg` in some livestreaming use cases. It allows for dynamic change of bitrate and for enabling a passthrough mode (to disable transcoding).
