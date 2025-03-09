# About

[![License: CC0-1.0](https://img.shields.io/badge/License-CC0%201.0-lightgrey.svg)](http://creativecommons.org/publicdomain/zero/1.0/)

This package is targeted on unlocking [`libav`](https://en.wikipedia.org/wiki/Libav) capabilities to build dynamic pipelines for processing audio/video.

For example:
```go
	ctx, cancelFn := context.WithCancel(ctx)

	// open the input

	input, err := avpipeline.NewInputFromURL(ctx, fromURL, "", avpipeline.InputConfig{})
	if err != nil {
		l.Fatal(err)
	}
	defer input.Close()
	inputNode := avpipeline.NewPipelineNode(input)

	// open the output

	output, err := avpipeline.NewOutputFromURL(
		ctx,
		toURL, "",
		avpipeline.OutputConfig{},
	)
	if err != nil {
		l.Fatal(err)
	}
	defer output.Close()
	outputNode := avpipeline.NewPipelineNode(output)

	// initialize a recorder to H264

	encoderFactory := avpipeline.NewNaiveEncoderFactory("libx264", "copy", 0, "")
	recoder, err := avpipeline.NewRecoder(
		ctx,
		avpipeline.NewNaiveDecoderFactory(0, ""),
		encoderFactory,
		nil,
	)
	if err != nil {
		l.Fatal(err)
	}
	defer recoder.Close()
	recodingNode := avpipeline.NewPipelineNode(recoder)

	// connect: input -> recoder -> output

	inputNode.PushTo = append(inputNode.PushTo, recodingNode)
	recodingNode.PushTo = append(recodingNode.PushTo, outputNode)

	// run

	pipeline := inputNode
	errCh := make(chan avpipeline.ErrPipeline, 10)
	observability.Go(ctx, func() {
		defer cancelFn()
		pipeline.Serve(ctx, avpipeline.PipelineServeConfig{
			FrameDrop: false,
		}, errCh)
	})

	// observe the progress

	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			l.Infof("finished")
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
				l.Fatal(err)
				return
			}
		case <-t.C:
			inputStats := inputNode.GetStats()
			inputStatsJSON, err := json.Marshal(inputStats)
			if err != nil {
				l.Fatal(err)
			}
			outputStats := finalNode.PushTo[0].GetStats()
			outputStatsJSON, err := json.Marshal(outputStats)
			if err != nil {
				l.Fatal(err)
			}
			fmt.Printf("input:%s -> output:%s\n", inputStatsJSON, outputStatsJSON)
		}
	}
```