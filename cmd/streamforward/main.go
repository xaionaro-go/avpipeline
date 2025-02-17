package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"strings"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/facebookincubator/go-belt/tool/logger/implementation/logrus"
	"github.com/spf13/pflag"
	"github.com/xaionaro-go/avpipeline"
	"github.com/xaionaro-go/observability"
)

func main() {
	pflag.Usage = func() {
		fmt.Fprintf(os.Stderr, "syntax: %s <URL-from> <URL-to>\n", os.Args[0])
	}

	loggerLevel := logger.LevelWarning
	pflag.Var(&loggerLevel, "log-level", "Log level")
	netPprofAddr := pflag.String("net-pprof-listen-addr", "", "an address to listen for incoming net/pprof connections")
	pflag.Parse()
	if len(pflag.Args()) != 2 {
		pflag.Usage()
		os.Exit(1)
	}

	l := logrus.Default().WithLevel(loggerLevel)
	ctx := logger.CtxWithLogger(context.Background(), l)
	ctx, cancelFn := context.WithCancel(ctx)
	logger.Default = func() logger.Logger {
		return l
	}
	defer belt.Flush(ctx)

	if *netPprofAddr != "" {
		observability.Go(ctx, func() { l.Error(http.ListenAndServe(*netPprofAddr, nil)) })
	}

	fromURL := pflag.Arg(0)
	toURL := pflag.Arg(1)

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

	l.Debugf("opening '%s' as the input...", fromURL)
	input, err := avpipeline.NewInputFromURL(ctx, fromURL, "", avpipeline.InputConfig{})
	if err != nil {
		l.Fatal(err)
	}

	l.Debugf("opening '%s' as the output...", toURL)
	output, err := avpipeline.NewOutputFromURL(
		ctx,
		toURL, "",
		avpipeline.OutputConfig{},
	)
	if err != nil {
		l.Fatal(err)
	}

	pipeline := avpipeline.NewPipelineNode(input)
	pipeline.PushTo = append(pipeline.PushTo, avpipeline.NewPipelineNode(output))
	observability.Go(ctx, func() {
		defer cancelFn()
		err := pipeline.Serve(ctx)
		if err != nil {
			l.Fatal(err)
		}
	})

	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			inputStats := pipeline.GetStats()
			inputStatsJSON, err := json.Marshal(inputStats)
			if err != nil {
				l.Fatal(err)
			}
			outputStats := pipeline.PushTo[0].GetStats()
			outputStatsJSON, err := json.Marshal(outputStats)
			if err != nil {
				l.Fatal(err)
			}
			fmt.Printf("input:%s -> output:%s\n", inputStatsJSON, outputStatsJSON)
		}
	}
}
