# About

[![License: CC0-1.0](https://img.shields.io/badge/License-CC0%201.0-lightgrey.svg)](http://creativecommons.org/publicdomain/zero/1.0/)

This package is targeted on unlocking [`libav`](https://en.wikipedia.org/wiki/Libav) capabilities to build dynamic pipelines for processing audio/video.

For example:
```go
	ctx, cancelFn := context.WithCancel(ctx)
	defer cancelFn()

	// input node

	logger.Debugf(ctx, "opening '%s' as the input...", fromURL)
	input, err := processor.NewInputFromURL(ctx, fromURL, secret.New(""), kernel.InputConfig{})
	assert(ctx, err == nil, err)
	defer input.Close(ctx)
	inputNode := avpipeline.NewNode(input)

	// output node

	logger.Debugf(ctx, "opening '%s' as the output...", toURL)
	output, err := processor.NewOutputFromURL(
		ctx,
		toURL, secret.New(""),
		kernel.OutputConfig{},
	)
	assert(ctx, err == nil, err)
	defer output.Close(ctx)
	outputNode := avpipeline.NewNode(output)

	// recoder node

	recoder, err := processor.NewRecoder(
		ctx,
		codec.NewNaiveDecoderFactory(ctx, 0, "", nil, nil),
		codec.NewNaiveEncoderFactory(ctx, "libx264", "copy", 0, "", nil, nil),
		nil,
	)
	assert(ctx, err == nil, err)
	defer recoder.Close(ctx)
	logger.Debugf(ctx, "initialized a recoder to H264...")
	recodingNode := avpipeline.NewNode(recoder)

	// route nodes: input -> recoder -> output

	inputNode.AddPushPacketsTo(recodingNode)
	recodingNode.AddPushPacketsTo(outputNode)
	logger.Debugf(ctx, "resulting pipeline: %s", inputNode.String())
	logger.Debugf(ctx, "resulting pipeline (for graphviz):\n%s\n", inputNode.DotString(false))

	// prepare (optionally)

	err := avpipeline.NotifyAboutPacketSourcesRecursively(ctx, nil, nodeInput)
	assert(ctx, err == nil, err)

	// start

	errCh := make(chan avpipeline.ErrNode, 10)
	observability.Go(ctx, func() {
		defer cancelFn()
		avpipeline.ServeRecursively(ctx, avpipeline.ServeConfig{
			FrameDrop: false,
		}, errCh, inputNode)
	})

	// observe

	statusTicker := time.NewTicker(time.Second)
	defer statusTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			logger.Infof(ctx, "finished")
			return
		case err, ok := <-errCh:
			if !ok {
				return
			}
			if errors.Is(err.Err, context.Canceled) {
				continue
			}
			if errors.Is(err.Err, io.EOF) {
				continue
			}
			if err.Err != nil {
				logger.Fatal(ctx, err)
				return
			}
		case <-statusTicker.C:
			inputStats := inputNode.GetStats()
			inputStatsJSON, err := json.Marshal(inputStats.FramesWrote)
			assert(ctx, err == nil, err)

			outputStats := outputNode.GetStats()
			outputStatsJSON, err := json.Marshal(outputStats.FramesRead)
			assert(ctx, err == nil, err)

			fmt.Printf("input:%s -> output:%s\n", inputStatsJSON, outputStatsJSON)
		}
	}
```

# Examples

* [`avd`](https://github.com/xaionaro-go/avd) users `avpipeline` to implement a streaming server (as an alternative to [`mediamtx`](https://github.com/bluenviron/mediamtx)).
* [`ffstream`](https://github.com/xaionaro-go/ffstream) uses `avpipeline` to implement a CLI that could be used as a kick-in replacement to `ffmpeg` in some livestreaming use cases. It allows for dynamic change of bitrate and for enabling a passthrough mode (to disable recoding).
