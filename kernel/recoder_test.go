package kernel_test

import (
	"context"
	"errors"
	"io"
	"path"
	"strings"
	"testing"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/pkg/runtime"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/facebookincubator/go-belt/tool/logger/implementation/logrus"
	"github.com/xaionaro-go/avpipeline"
	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/processor"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/secret"
)

func TestRecoderNoFailure(t *testing.T) {
	const vcodec = "libx264"
	const acodec = "copy"
	loggerLevel := logger.LevelTrace

	runtime.DefaultCallerPCFilter = observability.CallerPCFilter(runtime.DefaultCallerPCFilter)
	l := logrus.Default().WithLevel(loggerLevel)
	ctx := logger.CtxWithLogger(context.Background(), l)
	logger.Default = func() logger.Logger {
		return l
	}
	defer belt.Flush(ctx)

	toURL := "null"

	astiav.SetLogLevel(avpipeline.LogLevelToAstiav(l.Level()))
	astiav.SetLogCallback(func(c astiav.Classer, level astiav.LogLevel, fmt, msg string) {
		var cs string
		if c != nil {
			if cl := c.Class(); cl != nil {
				cs = " - class: " + cl.String()
			}
		}
		l.Logf(
			avpipeline.LogLevelFromAstiav(level),
			"%s%s",
			strings.TrimSpace(msg), cs,
		)
	})

	for _, fileName := range []string{"video0-1v1a.mov"} {
		t.Run(fileName, func(t *testing.T) {
			ctx, cancelFn := context.WithCancel(ctx)

			fromURL := path.Join("testdata", fileName)

			l.Debugf("opening '%s' as the input...", fromURL)
			input, err := kernel.NewInputFromURL(
				ctx,
				fromURL, secret.New(""),
				kernel.InputConfig{},
			)
			if err != nil {
				t.Fatal(err)
			}
			defer input.Close(ctx)

			l.Debugf("opening '%s' as the output...", toURL)
			output, err := kernel.NewOutputFromURL(
				ctx,
				toURL, secret.New(""),
				kernel.OutputConfig{
					CustomOptions: kernel.DictionaryItems{{
						Key:   "f",
						Value: "null",
					}},
				},
			)
			if err != nil {
				t.Fatal(err)
			}
			defer output.Close(ctx)

			errCh := make(chan avpipeline.ErrNode, 10)
			inputNode := avpipeline.NewNodeFromKernel(
				ctx,
				input,
				processor.OptionQueueSizeInput(1),
				processor.OptionQueueSizeOutput(1),
				processor.OptionQueueSizeError(2),
			)
			finalNode := inputNode
			encoderFactory := codec.NewNaiveEncoderFactory(vcodec, acodec, 0, "")
			recoder, err := kernel.NewRecoder(
				ctx,
				codec.NewNaiveDecoderFactory(0, ""),
				encoderFactory,
				nil,
			)
			if err != nil {
				t.Fatal(err)
			}
			defer recoder.Close(ctx)
			l.Debugf("initialized a recoder to %s (hwdev:%s)...", vcodec, "")
			recodingNode := avpipeline.NewNodeFromKernel(
				ctx,
				recoder,
				processor.OptionQueueSizeInput(100),
				processor.OptionQueueSizeOutput(1),
				processor.OptionQueueSizeError(2),
			)
			inputNode.PushTo.Add(recodingNode)
			finalNode = recodingNode
			finalNode.PushTo.Add(avpipeline.NewNodeFromKernel(
				ctx,
				output,
				processor.OptionQueueSizeInput(600),
				processor.OptionQueueSizeOutput(0),
				processor.OptionQueueSizeError(2),
			))

			l.Debugf("resulting pipeline: %s", inputNode.String())

			observability.Go(ctx, func() {
				defer cancelFn()
				avpipeline.ServeRecursively(ctx, inputNode, avpipeline.ServeConfig{
					FrameDrop: false,
				}, errCh)
			})

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
						t.Fatal(err)
						return
					}
				}
			}
		})
	}
}
