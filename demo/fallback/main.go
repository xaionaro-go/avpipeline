// main.go is the entry point for the fallback demo.

// Package main is a demo application.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	_ "net/http/pprof"
	"os"
	"time"

	"github.com/facebookincubator/go-belt"
	"github.com/spf13/pflag"
	"github.com/xaionaro-go/avpipeline"
	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/node"
	"github.com/xaionaro-go/avpipeline/preset/inputwithfallback"
	"github.com/xaionaro-go/avpipeline/processor"
	goconvavp "github.com/xaionaro-go/avpipeline/protobuf/goconv/avpipeline"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/secret"
)

func main() {
	// parse the input

	pflag.Usage = func() {
		fmt.Fprintf(os.Stderr, "syntax: %s <URL-from-main> <URL-from-fallback> <URL-to>\n", os.Args[0])
		pflag.PrintDefaults()
	}

	loggerLevel := logger.LevelWarning
	pflag.Var(&loggerLevel, "log-level", "Log level")
	netPprofAddr := pflag.String("net-pprof-listen-addr", "", "an address to listen for incoming net/pprof connections")
	frameDrop := pflag.Bool("framedrop", false, "")
	retryInterval := pflag.Duration("retry-interval", time.Second, "retry interval on input error")

	pflag.Parse()
	if len(pflag.Args()) != 3 {
		pflag.Usage()
		os.Exit(1)
	}

	fromURLMain := pflag.Arg(0)
	fromURLFallback := pflag.Arg(1)
	toURL := pflag.Arg(2)

	// init the context

	ctx := withLogger(context.Background(), loggerLevel)
	ctx, cancelFn := context.WithCancel(ctx)
	defer func() {
		logger.Debugf(ctx, "canceling context...")
		cancelFn()
	}()
	defer belt.Flush(ctx)

	if *netPprofAddr != "" {
		observability.Go(ctx, func(context.Context) { logger.Error(ctx, http.ListenAndServe(*netPprofAddr, nil)) })
	}

	defer logger.Infof(ctx, "finishing: finalization")

	inputs, err := inputwithfallback.New[*kernel.Input, codec.DecoderFactory, struct{}](
		ctx,
		[]inputwithfallback.InputFactory[*kernel.Input, codec.DecoderFactory, struct{}]{
			&inputFactory{URL: fromURLMain},
			&inputFactory{URL: fromURLFallback},
		},
		inputwithfallback.OptionRetryInterval(*retryInterval),
	)
	assert(ctx, err == nil, err)

	// output node

	logger.Debugf(ctx, "opening '%s' as the output...", toURL)
	output, err := processor.NewOutputFromURL(
		ctx,
		toURL, secret.New(""),
		kernel.OutputConfig{},
	)
	assert(ctx, err == nil, err)
	defer output.Close(belt.WithField(ctx, "reason", "program_end"))
	outputNode := node.New(output)

	defer logger.Infof(ctx, "finishing: nodes")

	inputs.GetOutput().AddPushTo(ctx, outputNode)

	defer logger.Infof(ctx, "finishing: routing")

	// start

	errCh := make(chan node.Error, 10)
	observability.Go(ctx, func(ctx context.Context) {
		defer func() {
			logger.Debugf(ctx, "cancelling context...")
			cancelFn()
		}()
		avpipeline.Serve[node.Abstract](ctx, avpipeline.ServeConfig{
			EachNode: node.ServeConfig{
				FrameDropVideo: *frameDrop,
				FrameDropAudio: *frameDrop,
				FrameDropOther: *frameDrop,
			},
		}, errCh, inputs)
	})
	logger.Infof(ctx, "started")

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
				logger.Infof(ctx, "closed")
				return
			}
			if errors.Is(err.Err, context.Canceled) {
				logger.Infof(ctx, "canceled")
				continue
			}
			if errors.Is(err.Err, io.EOF) {
				logger.Infof(ctx, "EOF")
				continue
			}
			if err.Err != nil {
				logger.Fatal(ctx, err)
				return
			}
		case <-statusTicker.C:

			fmt.Printf(
				"inputs: %s\ninput main: %s\ninput fallback: %s\noutput:%s\n",
				inputs,
				mustJSON(ctx, inputs.InputChains[0].Input.GetProcessor().CountersPtr().Generated.Packets.ToStats()),
				mustJSON(ctx, inputs.InputChains[1].Input.GetProcessor().CountersPtr().Generated.Packets.ToStats()),
				mustJSON(ctx, outputNode.GetCountersPtr().Received.Packets.ToStats()),
			)
			logger.Debugf(ctx, "main pipeline: %s", mustJSON(ctx, goconvavp.NodeToGRPC(ctx, inputs.InputChains[0].Input)))
			logger.Debugf(ctx, "fallback pipeline: %s", mustJSON(ctx, goconvavp.NodeToGRPC(ctx, inputs.InputChains[1].Input)))
		}
	}
}

func mustJSON(ctx context.Context, v any) string {
	b, err := json.Marshal(v)
	assert(ctx, err == nil, err)
	return string(b)
}
