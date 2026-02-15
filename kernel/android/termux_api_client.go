//go:build android

// termux_api_client.go provides direct Termux:API socket access.

package android

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/xaionaro-go/avpipeline/logger"
)

const (
	termuxAPISocketAddress     = "com.termux.api://listen"
	termuxAPISocketFallbackEnv = "TERMUX_API_SOCKET"
	termuxAPILibexecPath       = "/data/data/com.termux/files/usr/libexec/termux-api"
	termuxAPIBinaryName        = "termux-api"
)

type termuxAPIClient struct {
	DialTimeout time.Duration
}

func newTermuxAPIClient() *termuxAPIClient {
	return &termuxAPIClient{}
}

func (c *termuxAPIClient) Call(ctx context.Context, cmd termuxAPICommand) (termuxAPIResult, error) {
	output, err := c.callViaSocket(ctx, cmd)
	if err == nil {
		return termuxAPIResult{Raw: output}, nil
	}
	var unavailable ErrTermuxAPIUnavailable
	if !errors.As(err, &unavailable) {
		return termuxAPIResult{}, err
	}

	output, execErr := c.callViaExec(ctx, cmd)
	if execErr == nil {
		return termuxAPIResult{Raw: output}, nil
	}
	var cmdFailed ErrTermuxAPICommandFailed
	var responseErr ErrTermuxAPIResponse
	if errors.As(execErr, &cmdFailed) || errors.As(execErr, &responseErr) {
		return termuxAPIResult{}, execErr
	}
	return termuxAPIResult{}, ErrTermuxAPIUnavailable{Err: fmt.Errorf("socket: %v; exec: %v", err, execErr)}
}

func (c *termuxAPIClient) callViaSocket(ctx context.Context, cmd termuxAPICommand) (string, error) {
	commandLine, err := cmd.CommandLine()
	if err != nil {
		return "", err
	}

	conn, err := c.dial(ctx)
	if err != nil {
		return "", ErrTermuxAPIUnavailable{Err: err}
	}
	defer conn.Close()

	if err := writeTermuxCommand(conn, commandLine); err != nil {
		return "", ErrTermuxAPICommandFailed{Command: cmd.MethodName, Err: err}
	}

	output, err := readTermuxResponse(conn)
	if err != nil {
		return "", ErrTermuxAPIResponse{Err: err}
	}

	return output, nil
}

func (c *termuxAPIClient) callViaExec(ctx context.Context, cmd termuxAPICommand) (string, error) {
	args, err := cmd.CommandArgs()
	if err != nil {
		return "", err
	}
	path, err := termuxAPIBinaryPath()
	if err != nil {
		return "", ErrTermuxAPIUnavailable{Err: err}
	}
	command := exec.CommandContext(ctx, path, args...)
	output, err := command.CombinedOutput()
	if err != nil {
		return "", ErrTermuxAPICommandFailed{Command: cmd.MethodName, Message: strings.TrimSpace(string(output)), Err: err}
	}
	return string(output), nil
}

func termuxAPIBinaryPath() (string, error) {
	if _, err := os.Stat(termuxAPILibexecPath); err == nil {
		return termuxAPILibexecPath, nil
	}
	return exec.LookPath(termuxAPIBinaryName)
}

func (c *termuxAPIClient) dial(ctx context.Context) (net.Conn, error) {
	dialer := net.Dialer{}
	if c.DialTimeout > 0 {
		dialer.Timeout = c.DialTimeout
	}
	if addr := strings.TrimSpace(os.Getenv(termuxAPISocketFallbackEnv)); addr != "" {
		return dialer.DialContext(ctx, "unix", addr)
	}
	return dialer.DialContext(ctx, "unix", "\x00"+termuxAPISocketAddress)
}

func writeTermuxCommand(conn net.Conn, commandLine string) error {
	if len(commandLine) > int(^uint16(0)) {
		return fmt.Errorf("command line too long: %d bytes", len(commandLine))
	}

	length := make([]byte, 2)
	binary.BigEndian.PutUint16(length, uint16(len(commandLine)))
	if _, err := conn.Write(length); err != nil {
		return err
	}
	_, err := conn.Write([]byte(commandLine))
	return err
}

func readTermuxResponse(conn net.Conn) (string, error) {
	buffer := make([]byte, 4096)
	var output strings.Builder
	for {
		n, err := conn.Read(buffer)
		if n > 0 {
			output.Write(buffer[:n])
		}
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) || errors.Is(err, context.Canceled) {
				return output.String(), nil
			}
			if errors.Is(err, context.DeadlineExceeded) {
				return output.String(), err
			}
			return output.String(), err
		}
		if n == 0 {
			return output.String(), nil
		}
	}
}

type termuxAPIResult struct {
	Raw string
}

type termuxAPICommand struct {
	MethodName string
	Action     string
	Extras     []termuxAPIExtra
}

func (c termuxAPICommand) CommandLine() (string, error) {
	if c.MethodName == "" {
		return "", ErrTermuxAPIResponse{Message: "missing method name"}
	}
	var builder strings.Builder
	writeToken(&builder, "--es", "api_method", c.MethodName)
	if c.Action != "" {
		if strings.ContainsAny(c.Action, " \t\n") {
			return "", ErrTermuxAPIResponse{Message: fmt.Sprintf("invalid action: %q", c.Action)}
		}
		writeAction(&builder, c.Action)
	}
	for _, extra := range c.Extras {
		if err := extra.Write(&builder); err != nil {
			return "", err
		}
	}
	return strings.TrimSpace(builder.String()), nil
}

func (c termuxAPICommand) CommandArgs() ([]string, error) {
	if c.MethodName == "" {
		return nil, ErrTermuxAPIResponse{Message: "missing method name"}
	}
	args := []string{c.MethodName}
	if c.Action != "" {
		if strings.ContainsAny(c.Action, " \t\n") {
			return nil, ErrTermuxAPIResponse{Message: fmt.Sprintf("invalid action: %q", c.Action)}
		}
		args = append(args, "-a", c.Action)
	}
	for _, extra := range c.Extras {
		var err error
		args, err = extra.appendArgs(args)
		if err != nil {
			return nil, err
		}
	}
	return args, nil
}

type termuxAPIExtra struct {
	Key   string
	Value termuxAPIValue
}

func (e termuxAPIExtra) Write(builder *strings.Builder) error {
	if e.Key == "" {
		return ErrTermuxAPIResponse{Message: "empty extra key"}
	}
	if e.Value == nil {
		return ErrTermuxAPIResponse{Message: "nil extra value"}
	}
	return e.Value.Write(builder, e.Key)
}

func (e termuxAPIExtra) appendArgs(args []string) ([]string, error) {
	if e.Key == "" {
		return args, ErrTermuxAPIResponse{Message: "empty extra key"}
	}
	if e.Value == nil {
		return args, ErrTermuxAPIResponse{Message: "nil extra value"}
	}
	switch v := e.Value.(type) {
	case termuxAPIString:
		return append(args, "--es", e.Key, string(v)), nil
	case termuxAPIInt:
		return append(args, "--ei", e.Key, strconv.Itoa(int(v))), nil
	case termuxAPIBool:
		value := "false"
		if v {
			value = "true"
		}
		return append(args, "--ez", e.Key, value), nil
	case termuxAPIStringArray:
		return append(args, "--esa", e.Key, escapeTermuxStringList(v)), nil
	default:
		return args, ErrTermuxAPIResponse{Message: "unsupported extra value"}
	}
}

type termuxAPIValue interface {
	Write(builder *strings.Builder, key string) error
}

type termuxAPIString string

func (v termuxAPIString) Write(builder *strings.Builder, key string) error {
	writeToken(builder, "--es", key, string(v))
	return nil
}

type termuxAPIInt int

func (v termuxAPIInt) Write(builder *strings.Builder, key string) error {
	builder.WriteString("--ei ")
	builder.WriteString(escapeTermuxValue(key))
	builder.WriteString(" ")
	builder.WriteString(strconv.Itoa(int(v)))
	builder.WriteString(" ")
	return nil
}

type termuxAPIBool bool

func (v termuxAPIBool) Write(builder *strings.Builder, key string) error {
	builder.WriteString("--ez ")
	builder.WriteString(escapeTermuxValue(key))
	builder.WriteString(" ")
	if v {
		builder.WriteString("true")
	} else {
		builder.WriteString("false")
	}
	builder.WriteString(" ")
	return nil
}

type termuxAPIStringArray []string

func (v termuxAPIStringArray) Write(builder *strings.Builder, key string) error {
	writeToken(builder, "--esa", key, escapeTermuxStringList(v))
	return nil
}

func writeToken(builder *strings.Builder, flag, key, value string) {
	builder.WriteString(flag)
	if key != "" {
		builder.WriteString(" ")
		builder.WriteString(escapeTermuxValue(key))
	}
	builder.WriteString(" ")
	builder.WriteString("\"")
	builder.WriteString(escapeTermuxString(value))
	builder.WriteString("\" ")
}

func writeAction(builder *strings.Builder, action string) {
	builder.WriteString("-a ")
	builder.WriteString(action)
	builder.WriteString(" ")
}

func escapeTermuxString(input string) string {
	replaced := strings.ReplaceAll(input, "\\", "\\\\")
	replaced = strings.ReplaceAll(replaced, "\"", "\\\"")
	return replaced
}

func escapeTermuxStringList(items []string) string {
	if len(items) == 0 {
		return ""
	}
	escaped := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.ReplaceAll(item, "\\", "\\\\")
		item = strings.ReplaceAll(item, ",", "\\,")
		item = strings.ReplaceAll(item, "\"", "\\\"")
		escaped = append(escaped, item)
	}
	return strings.Join(escaped, ",")
}

func escapeTermuxValue(input string) string {
	if input == "" {
		return "\"\""
	}
	return input
}

func logTermuxResult(ctx context.Context, method string, result termuxAPIResult) {
	logger.Tracef(ctx, "termux api result for %s: %s", method, strings.TrimSpace(result.Raw))
}
