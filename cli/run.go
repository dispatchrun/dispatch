package cli

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	sdkv1 "buf.build/gen/go/stealthrocket/dispatch-proto/protocolbuffers/go/dispatch/sdk/v1"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/proto"
)

var (
	BridgeSession string
	LocalEndpoint string
	Verbose       bool
)

const defaultEndpoint = "127.0.0.1:8000"

const (
	pollTimeout    = 30 * time.Second
	cleanupTimeout = 5 * time.Second
)

var httpClient = &http.Client{
	Transport: http.DefaultTransport,
	Timeout:   pollTimeout,
}

var (
	dispatchLogPrefixStyle  = lipgloss.NewStyle().Foreground(greenColor)
	appLogPrefixStyle       = lipgloss.NewStyle().Foreground(magentaColor)
	logPrefixSeparatorStyle = lipgloss.NewStyle().Foreground(grayColor)
)

func runCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run a Dispatch application",
		Long: fmt.Sprintf(`Run a Dispatch application.

The command to start the local application endpoint should be
specified after the run command and its options:

  dispatch run [options] -- <command>

Dispatch spawns the local application endpoint and then dispatches
function calls to it continuously.

Dispatch connects to the local application endpoint on http://%s.
If the local application is listening on a different host or port,
please set the --endpoint option appropriately. The value passed to
this option will be exported as the DISPATCH_ENDPOINT_ADDR environment
variable to the local application.

A new session is created each time the command is run. A session is
a pristine environment in which function calls can be dispatched and
handled by the local application. To start the command using a previous
session, use the --session option to specify a session ID from a
previous run.`, defaultEndpoint),
		Args:         cobra.MinimumNArgs(1),
		SilenceUsage: true,
		GroupID:      "dispatch",
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return runConfigFlow()
		},
		RunE: func(c *cobra.Command, args []string) error {
			arg0 := filepath.Base(args[0])

			prefixWidth := max(len("dispatch"), len(arg0))

			if checkEndpoint(LocalEndpoint, time.Second) {
				return fmt.Errorf("cannot start local application on address that's already in use: %v", LocalEndpoint)
			}

			// Enable the TUI if this is an interactive session and
			// stdout/stderr aren't redirected.
			var tui *TUI
			var logWriter io.Writer = os.Stderr
			var observer FunctionCallObserver
			if isTerminal(os.Stdin) && isTerminal(os.Stdout) && isTerminal(os.Stderr) {
				tui = &TUI{}
				logWriter = tui
				observer = tui
			}

			// Add a prefix to Dispatch logs.
			level := slog.LevelInfo
			if Verbose {
				level = slog.LevelDebug
			}
			slog.SetDefault(slog.New(&slogHandler{
				stream: &prefixLogWriter{
					stream: logWriter,
					prefix: []byte(dispatchLogPrefixStyle.Render(pad("dispatch", prefixWidth)) + logPrefixSeparatorStyle.Render(" | ")),
				},
				level: level,
			}))

			if BridgeSession == "" {
				BridgeSession = uuid.New().String()
			}

			if !Verbose && tui == nil {
				dialog(`Starting Dispatch session: %v

Run 'dispatch help run' to learn about Dispatch sessions.`, BridgeSession)
			}

			slog.Info("starting session", "session_id", BridgeSession)

			// Execute the command, forwarding the environment and
			// setting the necessary extra DISPATCH_* variables.
			cmd := exec.Command(args[0], args[1:]...)

			cmd.Stdin = os.Stdin

			// Pipe stdout/stderr streams through a writer that adds a prefix,
			// so that it's easier to disambiguate Dispatch logs from the local
			// application's logs.
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				return fmt.Errorf("failed to create stdout pipe: %v", err)
			}
			defer stdout.Close()

			stderr, err := cmd.StderrPipe()
			if err != nil {
				return fmt.Errorf("failed to create stderr pipe: %v", err)
			}
			defer stderr.Close()

			// Pass on environment variables to the local application.
			// Pass on the configured API key, and set a special endpoint
			// URL for the session. Unset the verification key, so that
			// it doesn't conflict with the session. A verification key
			// is not required here, since function calls are retrieved
			// from an authenticated API endpoint.
			cmd.Env = append(
				withoutEnv(os.Environ(), "DISPATCH_VERIFICATION_KEY="),
				"DISPATCH_API_KEY="+DispatchApiKey,
				"DISPATCH_ENDPOINT_URL=bridge://"+BridgeSession,
				"DISPATCH_ENDPOINT_ADDR="+LocalEndpoint,
			)

			// Set OS-specific process attributes.
			cmd.SysProcAttr = &syscall.SysProcAttr{}
			setSysProcAttr(cmd.SysProcAttr)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			var wg sync.WaitGroup

			// Setup signal handler.
			signals := make(chan os.Signal, 2)
			signal.Notify(signals, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM)
			var signaled bool
			wg.Add(1)
			go func() {
				defer wg.Done()

				for {
					select {
					case <-ctx.Done():
						return
					case <-signals:
						if !signaled {
							signaled = true
							_ = cmd.Process.Signal(syscall.SIGTERM)
						} else {
							_ = cmd.Process.Kill()
						}
					}
				}
			}()

			// Initialize the TUI.
			if tui != nil {
				p := tea.NewProgram(tui,
					tea.WithContext(ctx),
					tea.WithoutSignalHandler(),
					tea.WithoutCatchPanics())
				wg.Add(1)
				go func() {
					defer wg.Done()

					if _, err := p.Run(); err != nil && !errors.Is(err, tea.ErrProgramKilled) {
						panic(err)
					}
					// Quitting the TUI sends an implicit interrupt.
					select {
					case signals <- syscall.SIGINT:
					default:
					}
				}()
			}

			bridgeSessionURL := fmt.Sprintf("%s/sessions/%s", DispatchBridgeUrl, BridgeSession)

			// Poll for work in the background.
			var successfulPolls int64
			wg.Add(1)
			go func() {
				defer wg.Done()

				for ctx.Err() == nil {
					// Fetch a request from the API.
					requestID, res, err := poll(ctx, httpClient, bridgeSessionURL)
					if err != nil {
						if ctx.Err() != nil {
							return
						}
						switch e := err.(type) {
						case authError:
							failure(e.Error())
							return
						default:
							slog.Warn(err.Error())
						}
						time.Sleep(1 * time.Second)
						continue
					} else if res == nil {
						continue
					}

					atomic.AddInt64(&successfulPolls, +1)

					// Asynchronously send the request to invoke a function to
					// the local application.
					wg.Add(1)
					go func() {
						defer wg.Done()

						err := invoke(ctx, httpClient, bridgeSessionURL, requestID, res, observer)
						res.Body.Close()
						if err != nil {
							if ctx.Err() == nil {
								slog.Warn(err.Error())
							}

							// Notify upstream if we're unable to generate a response,
							// either because the local application can't be contacted,
							// is misbehaving, or a shutdown sequence has been initiated.
							ctx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
							defer cancel()
							if err := cleanup(ctx, httpClient, bridgeSessionURL, requestID); err != nil {
								slog.Debug(err.Error())
							}
						}
					}()
				}
			}()

			if err = cmd.Start(); err != nil {
				return fmt.Errorf("failed to start %s: %v", strings.Join(args, " "), err)
			}

			// Add a prefix to the local application's logs.
			appLogPrefix := []byte(appLogPrefixStyle.Render(pad(arg0, prefixWidth)) + logPrefixSeparatorStyle.Render(" | "))
			go printPrefixedLines(logWriter, stdout, appLogPrefix)
			go printPrefixedLines(logWriter, stderr, appLogPrefix)

			err = cmd.Wait()

			// Cancel the context and wait for all goroutines to return.
			cancel()
			wg.Wait()

			// If the command was halted by a signal rather than some other error,
			// assume that the command invocation succeeded and that the user may
			// want to resume this session.
			if signaled {
				err = nil

				if atomic.LoadInt64(&successfulPolls) > 0 && !Verbose {
					dispatchArg0 := os.Args[0]
					dialog("To resume this Dispatch session:\n\n\t%s run --session %s -- %s",
						dispatchArg0, BridgeSession, strings.Join(args, " "))
				}
			}

			if err != nil {
				dumpLogs(logWriter)
				return fmt.Errorf("failed to invoke command '%s': %v", strings.Join(args, " "), err)
			} else if !signaled && successfulPolls == 0 {
				dumpLogs(logWriter)
				return fmt.Errorf("command '%s' exited unexpectedly", strings.Join(args, " "))
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&BridgeSession, "session", "s", "", "Optional session to resume")
	cmd.Flags().StringVarP(&LocalEndpoint, "endpoint", "e", defaultEndpoint, "Host:port that the local application endpoint is listening on")
	cmd.Flags().BoolVarP(&Verbose, "verbose", "", false, "Enable verbose logging")

	return cmd
}

func dumpLogs(logWriter io.Writer) {
	if r, ok := logWriter.(io.Reader); ok {
		time.Sleep(100 * time.Millisecond)
		_, _ = io.Copy(os.Stderr, r)
		_, _ = os.Stderr.Write([]byte{'\n'})
	}
}

func poll(ctx context.Context, client *http.Client, url string) (string, *http.Response, error) {
	slog.Debug("getting request from Dispatch", "url", url)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		panic(err)
	}
	req.Header.Add("Authorization", "Bearer "+DispatchApiKey)
	req.Header.Add("Request-Timeout", strconv.FormatInt(int64(pollTimeout.Seconds()), 10))
	if DispatchBridgeHostHeader != "" {
		req.Host = DispatchBridgeHostHeader
	}

	res, err := client.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("failed to contact Dispatch API (%s): %v", DispatchBridgeUrl, err)
	}
	if res.StatusCode != http.StatusOK {
		res.Body.Close()

		switch res.StatusCode {
		case http.StatusUnauthorized:
			return "", nil, authError{}
		case http.StatusGatewayTimeout:
			// A 504 is expected when long polling and no requests
			// are available. Return a nil in this case and let the
			// caller try again.
			return "", nil, nil
		default:
			return "", nil, fmt.Errorf("failed to contact Dispatch API (%s): response code %d", DispatchBridgeUrl, res.StatusCode)
		}
	}

	requestID := res.Header.Get("X-Request-Id")

	return requestID, res, nil
}

// FunctionCallObserver observes function call requests and responses.
//
// The observer may be invoked concurrently from many goroutines.
type FunctionCallObserver interface {
	// ObserveRequest observes a RunRequest as it passes from the API through
	// the CLI to the local application.
	ObserveRequest(*sdkv1.RunRequest)

	// ObserveResponse observes a response to the RunRequest.
	//
	// If the RunResponse is nil, it means the local application did not return
	// a valid response. If the http.Response is not nil, it means an HTTP
	// response was generated, but it wasn't a valid RunResponse. The error may
	// be present if there was either an error making the HTTP request, or parsing
	// the response.
	//
	// ObserveResponse always comes after a call to ObserveRequest for any given
	// RunRequest.
	ObserveResponse(*sdkv1.RunRequest, error, *http.Response, *sdkv1.RunResponse)
}

func invoke(ctx context.Context, client *http.Client, url, requestID string, bridgeGetRes *http.Response, observer FunctionCallObserver) error {
	logger := slog.Default()
	if Verbose {
		logger = slog.With("request_id", requestID)
	}

	logger.Debug("sending request to local application", "endpoint", LocalEndpoint)

	// Extract the nested request header/body.
	endpointReq, err := http.ReadRequest(bufio.NewReader(bridgeGetRes.Body))
	if err != nil {
		return fmt.Errorf("invalid response from Dispatch API: %v", err)
	}
	endpointReq = endpointReq.WithContext(ctx)

	// Buffer the request body in memory.
	endpointReqBody := &bytes.Buffer{}
	if endpointReq.ContentLength > 0 {
		endpointReqBody.Grow(int(endpointReq.ContentLength))
	}
	endpointReq.ContentLength, err = io.Copy(endpointReqBody, endpointReq.Body)
	endpointReq.Body.Close()
	bridgeGetRes.Body.Close()
	if err != nil {
		return fmt.Errorf("failed to read response from Dispatch API: %v", err)
	}
	endpointReq.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(endpointReqBody.Bytes())), nil
	}
	endpointReq.Body, _ = endpointReq.GetBody()

	// Parse the request body from the API.
	var runRequest sdkv1.RunRequest
	if err := proto.Unmarshal(endpointReqBody.Bytes(), &runRequest); err != nil {
		return fmt.Errorf("invalid response from Dispatch API: %v", err)
	}
	logger.Debug("parsed request", "function", runRequest.Function, "dispatch_id", runRequest.DispatchId)
	switch runRequest.Directive.(type) {
	case *sdkv1.RunRequest_Input:
		logger.Info("calling function", "function", runRequest.Function)
	case *sdkv1.RunRequest_PollResult:
		logger.Info("resuming function", "function", runRequest.Function)
	}
	if observer != nil {
		observer.ObserveRequest(&runRequest)
	}

	// The RequestURI field must be cleared for client.Do() to
	// accept the request below.
	endpointReq.RequestURI = ""

	// Forward the request to the local application endpoint.
	endpointReq.Host = LocalEndpoint
	endpointReq.URL.Scheme = "http"
	endpointReq.URL.Host = LocalEndpoint
	endpointRes, err := client.Do(endpointReq)
	if err != nil {
		if observer != nil {
			observer.ObserveResponse(&runRequest, err, nil, nil)
		}
		return fmt.Errorf("failed to contact local application endpoint (%s): %v. Please check that -e,--endpoint is correct.", LocalEndpoint, err)
	}

	// Buffer the response body in memory.
	endpointResBody := &bytes.Buffer{}
	if endpointRes.ContentLength > 0 {
		endpointResBody.Grow(int(endpointRes.ContentLength))
	}
	_, err = io.Copy(endpointResBody, endpointRes.Body)
	endpointRes.Body.Close()
	if err != nil {
		if observer != nil {
			observer.ObserveResponse(&runRequest, err, endpointRes, nil)
		}
		return fmt.Errorf("failed to read response from local application endpoint (%s): %v", LocalEndpoint, err)
	}
	endpointRes.Body = io.NopCloser(endpointResBody)
	endpointRes.ContentLength = int64(endpointResBody.Len())

	// Parse the response body from the API.
	if endpointRes.StatusCode == http.StatusOK && endpointRes.Header.Get("Content-Type") == "application/proto" {
		var runResponse sdkv1.RunResponse
		if err := proto.Unmarshal(endpointResBody.Bytes(), &runResponse); err != nil {
			if observer != nil {
				observer.ObserveResponse(&runRequest, err, endpointRes, nil)
			}
			return fmt.Errorf("invalid response from local application endpoint (%s): %v", LocalEndpoint, err)
		}
		switch runResponse.Status {
		case sdkv1.Status_STATUS_OK:
			switch d := runResponse.Directive.(type) {
			case *sdkv1.RunResponse_Exit:
				if d.Exit.TailCall != nil {
					logger.Info("function tail-called", "function", runRequest.Function, "tail_call", d.Exit.TailCall.Function)
				} else {
					logger.Info("function call succeeded", "function", runRequest.Function)
				}
			case *sdkv1.RunResponse_Poll:
				logger.Info("function yielded", "function", runRequest.Function, "calls", len(d.Poll.Calls))
			}
		default:
			err := runResponse.GetExit().GetResult().GetError()
			logger.Warn("function call failed", "function", runRequest.Function, "status", statusString(runResponse.Status), "error_type", err.GetType(), "error_message", err.GetMessage())
		}
		if observer != nil {
			observer.ObserveResponse(&runRequest, nil, endpointRes, &runResponse)
		}
	} else {
		// The response might indicate some other issue, e.g. it could be a 404 if the function can't be found
		logger.Warn("function call failed", "function", runRequest.Function, "http_status", endpointRes.StatusCode)
		if observer != nil {
			observer.ObserveResponse(&runRequest, nil, endpointRes, nil)
		}
	}

	// Use io.Pipe to convert the response writer into an io.Reader.
	pr, pw := io.Pipe()
	go func() {
		err := endpointRes.Write(pw)
		pw.CloseWithError(err)
	}()

	logger.Debug("sending response to Dispatch")

	// Send the response back to the API.
	bridgePostReq, err := http.NewRequestWithContext(ctx, "POST", url, bufio.NewReader(pr))
	if err != nil {
		panic(err)
	}
	bridgePostReq.Header.Add("Authorization", "Bearer "+DispatchApiKey)
	bridgePostReq.Header.Add("X-Request-ID", requestID)
	if DispatchBridgeHostHeader != "" {
		bridgePostReq.Host = DispatchBridgeHostHeader
	}
	bridgePostRes, err := client.Do(bridgePostReq)
	if err != nil {
		return fmt.Errorf("failed to contact Dispatch API or send response: %v", err)
	}
	switch bridgePostRes.StatusCode {
	case http.StatusAccepted:
		return nil
	case http.StatusNotFound:
		// A 404 is expected if there's a timeout upstream that's hit
		// before the response can be sent.
		logger.Debug("request is no longer available", "method", "post")
		return nil
	default:
		return fmt.Errorf("failed to contact Dispatch API to send response: response code %d", bridgePostRes.StatusCode)
	}
}

func cleanup(ctx context.Context, client *http.Client, url, requestID string) error {
	slog.Debug("cleaning up request", "request_id", requestID)

	req, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		panic(err)
	}
	req.Header.Add("Authorization", "Bearer "+DispatchApiKey)
	req.Header.Add("X-Request-ID", requestID)
	if DispatchBridgeHostHeader != "" {
		req.Host = DispatchBridgeHostHeader
	}
	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to contact Dispatch API to cleanup request: %v", err)
	}
	switch res.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusNotFound:
		// A 404 can occur if the request is cleaned up concurrently, either
		// because a response was received upstream but the CLI didn't realize
		// the response went through, or because a timeout was reached upstream.
		slog.Debug("request is no longer available", "request_id", requestID, "method", "delete")
		return nil
	default:
		return fmt.Errorf("failed to contact Dispatch API to cleanup request: response code %d", res.StatusCode)
	}
}

func checkEndpoint(addr string, timeout time.Duration) bool {
	slog.Debug("checking endpoint", "addr", addr)
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		slog.Debug("endpoint could not be contacted", "addr", addr, "err", err)
		return false
	}
	slog.Debug("endpoint contacted successfully", "addr", addr)
	conn.Close()
	return true
}

func withoutEnv(env []string, prefixes ...string) []string {
	return slices.DeleteFunc(env, func(v string) bool {
		for _, prefix := range prefixes {
			if strings.HasPrefix(v, prefix) {
				return true
			}
		}
		return false
	})
}

func printPrefixedLines(w io.Writer, r io.Reader, prefix []byte) {
	scanner := bufio.NewScanner(r)
	buffer := bytes.NewBuffer(nil)
	buffer.Write(prefix)

	for scanner.Scan() {
		buffer.Truncate(len(prefix))
		buffer.Write(scanner.Bytes())
		buffer.WriteByte('\n')
		_, _ = w.Write(buffer.Bytes())
	}
}

func pad(s string, width int) string {
	if len(s) < width {
		return s + strings.Repeat(" ", width-len(s))
	}
	return s
}
