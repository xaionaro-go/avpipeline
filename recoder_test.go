package avpipeline_test

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
	"github.com/xaionaro-go/observability"
)

func TestRecoderNoFailure(t *testing.T) {
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

	for _, fileName := range []string{"video0-1v1a.mov", "video0-1v6a.mov"} {
		t.Run(fileName, func(t *testing.T) {
			ctx, cancelFn := context.WithCancel(ctx)

			fromURL := path.Join("testdata", fileName)

			l.Debugf("opening '%s' as the input...", fromURL)
			input, err := avpipeline.NewInputFromURL(ctx, fromURL, "", avpipeline.InputConfig{})
			if err != nil {
				t.Fatal(err)
			}
			defer input.Close()

			l.Debugf("opening '%s' as the output...", toURL)
			output, err := avpipeline.NewOutputFromURL(
				ctx,
				toURL, "",
				avpipeline.OutputConfig{
					CustomOptions: avpipeline.DictionaryItems{{
						Key:   "f",
						Value: "null",
					}},
				},
			)
			if err != nil {
				t.Fatal(err)
			}
			defer output.Close()

			errCh := make(chan avpipeline.ErrPipeline, 10)
			inputNode := avpipeline.NewPipelineNode(input)
			finalNode := inputNode
			encoderFactory := avpipeline.NewNaiveEncoderFactory("libx264", "copy", 0, "")
			recoder, err := avpipeline.NewRecoder(
				ctx,
				avpipeline.NewNaiveDecoderFactory(0, ""),
				encoderFactory,
				nil,
			)
			if err != nil {
				t.Fatal(err)
			}
			defer recoder.Close()
			l.Debugf("initialized a recoder to %s (hwdev:%s)...", "libx264", "")
			recodingNode := avpipeline.NewPipelineNode(recoder)
			inputNode.PushTo.Add(recodingNode)
			finalNode = recodingNode
			finalNode.PushTo.Add(avpipeline.NewPipelineNode(output))

			pipeline := inputNode
			l.Debugf("resulting pipeline: %s", pipeline.String())

			observability.Go(ctx, func() {
				defer cancelFn()
				pipeline.Serve(ctx, avpipeline.PipelineServeConfig{
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
