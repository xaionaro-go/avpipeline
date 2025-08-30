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

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/pkg/field"
	"github.com/spf13/pflag"
	"github.com/xaionaro-go/avpipeline"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/node"
	packetfiltercondition "github.com/xaionaro-go/avpipeline/node/filter/packetfilter/condition"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packet/condition"
	"github.com/xaionaro-go/avpipeline/packet/filter"
	"github.com/xaionaro-go/avpipeline/preset/autoheaders"
	"github.com/xaionaro-go/avpipeline/processor"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/observability/xlogger"
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

	// input nodes: main

	logger.Debugf(ctx, "opening '%s' as the main input...", fromURLMain)

	sw := condition.NewSwitch()
	sw.SetValue(ctx, 1)
	tsShifter := filter.NewShiftTimestamps(0, nil)
	inputMain := kernel.NewRetry(xlogger.CtxWithMaxLoggingLevel(ctx, logger.LevelWarning),
		func(ctx context.Context) (*kernel.Input, error) {
			return kernel.NewInputFromURL(ctx, fromURLMain, secret.New(""), kernel.InputConfig{})
		},
		func(ctx context.Context, k *kernel.Input) error {
			sw.SetValue(ctx, 0)
			return nil
		},
		func(ctx context.Context, k *kernel.Input, err error) error {
			sw.SetValue(ctx, 1)
			time.Sleep(time.Second)
			return kernel.ErrRetry{Err: err}
		},
	)
	defer inputMain.Close(belt.WithFields(ctx, field.Map[string]{"reason": "program_end", "input": "main"}))
	inputMainNode := node.NewFromKernel(ctx, inputMain)

	// input nodes: fallback

	logger.Debugf(ctx, "opening '%s' as the fallback input...", fromURLFallback)
	inputFallback, err := processor.NewInputFromURL(ctx, fromURLFallback, secret.New(""), kernel.InputConfig{})
	assert(ctx, err == nil, err)
	defer inputFallback.Close(belt.WithFields(ctx, field.Map[string]{"reason": "program_end", "input": "fallback"}))
	inputFallbackNode := node.New(inputFallback)

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

	// route nodes:
	//
	//                AnnexB BSF + ShiftTimestamps
	// input (main) ->---------------------------+
	//                                           +--> output
	// input (fallback) ->-----------------------+
	//                     AnnexB BitStreamFilter

	outputFormatName := outputNode.Processor.Kernel.FormatContext.OutputFormat().Name()
	logger.Infof(ctx, "output format: '%s'", outputFormatName)

	if outputFormatName != "mpegts" {
		logger.Fatalf(ctx, "fallback is supported only for mpegts output format")
	}

	nodeBSFMain := autoheaders.NewNode(
		ctx,
		outputNode.Processor.GetPacketSink(),
	)
	nodeBSFFallback := autoheaders.NewNode(
		ctx,
		outputNode.Processor.GetPacketSink(),
	)

	var filteredInputMain node.Abstract = inputMainNode
	if nodeBSFMain != nil {
		logger.Debugf(ctx, "inserting %s to the main pipeline", nodeBSFMain.Processor.Kernel)
		filteredInputMain.AddPushPacketsTo(nodeBSFMain)
		filteredInputMain = nodeBSFMain
	}

	var filteredInputFallback node.Abstract = inputFallbackNode
	if nodeBSFFallback != nil {
		logger.Debugf(ctx, "inserting %s to the fallback pipeline", nodeBSFFallback.Processor.Kernel)
		filteredInputFallback.AddPushPacketsTo(nodeBSFFallback)
		filteredInputFallback = nodeBSFFallback
	}

	lastDTS := int64(0)
	filteredInputMain.AddPushPacketsTo(
		outputNode,
		packetfiltercondition.Packet{
			Condition: condition.And{
				sw.PacketCondition(0),
				tsShifter,
				condition.Function(func(ctx context.Context, pkt packet.Input) bool {
					if pkt.CodecParameters().MediaType() != astiav.MediaTypeVideo {
						return true
					}
					if !pkt.Flags().Has(astiav.PacketFlagKey) {
						return true
					}
					if pkt.Dts() > lastDTS {
						lastDTS = pkt.Dts()
					}
					if pkt.Dts() < lastDTS {
						tsShifter.Offset += lastDTS - pkt.Dts()
					}
					return true
				}),
			},
		},
	)
	filteredInputFallback.AddPushPacketsTo(
		outputNode,
		packetfiltercondition.Packet{
			Condition: condition.And{
				sw.PacketCondition(1),
				condition.Function(func(ctx context.Context, pkt packet.Input) bool {
					if pkt.CodecParameters().MediaType() != astiav.MediaTypeVideo {
						return true
					}
					if !pkt.Flags().Has(astiav.PacketFlagKey) {
						return true
					}
					if pkt.Dts() > lastDTS {
						lastDTS = pkt.Dts()
					}
					return true
				}),
			},
		},
	)
	pipelineInputs := node.Nodes[node.Abstract]{
		inputMainNode,
		inputFallbackNode,
	}
	logger.Debugf(ctx, "resulting pipeline: %s", pipelineInputs.String())
	logger.Debugf(ctx, "resulting pipeline (for graphviz):\n%s\n", pipelineInputs.DotString(false))

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
				FrameDrop: *frameDrop,
			},
		}, errCh, pipelineInputs...)
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
			inputMainStats := inputMainNode.GetCountersPtr()
			inputMainStatsJSON, err := json.Marshal(inputMainStats.Packets.Sent)
			assert(ctx, err == nil, err)

			inputFallbackStats := inputFallbackNode.GetCountersPtr()
			inputFallbackStatsJSON, err := json.Marshal(inputFallbackStats.Packets.Sent)
			assert(ctx, err == nil, err)

			outputStats := outputNode.GetCountersPtr()
			outputStatsJSON, err := json.Marshal(outputStats.Packets.Received)
			assert(ctx, err == nil, err)

			fmt.Printf("input-main:%s + input-fallback:%s -> output:%s\n", inputMainStatsJSON, inputFallbackStatsJSON, outputStatsJSON)
		}
	}
}
