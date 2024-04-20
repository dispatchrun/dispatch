package cli

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
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

	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/proto"

	sdkv1 "buf.build/gen/go/stealthrocket/dispatch-proto/protocolbuffers/go/dispatch/sdk/v1"
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

			if Verbose {
				prefix := []byte(pad("dispatch", prefixWidth) + " | ")
				if Color {
					prefix = []byte("\033[32m" + pad("dispatch", prefixWidth) + " \033[90m|\033[0m ")
				}
				// Print Dispatch logs with a prefix in verbose mode.
				slog.SetDefault(slog.New(&prefixHandler{
					Handler: slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}),
					stream:  os.Stderr,
					prefix:  prefix,
				}))
			}

			if BridgeSession == "" {
				BridgeSession = uuid.New().String()
			}

			if Verbose {
				slog.Info("starting session", "session_id", BridgeSession)
			} else {
				dialog(`Starting Dispatch session: %v

Run 'dispatch help run' to learn about Dispatch sessions.`, BridgeSession)
			}

			// Execute the command, forwarding the environment and
			// setting the necessary extra DISPATCH_* variables.
			cmd := exec.Command(args[0], args[1:]...)

			cmd.Stdin = os.Stdin

			// When verbose logging is enabled, pipe stdout/stderr streams
			// through a writer that adds a prefix, so that it's easier
			// to disambiguate Dispatch logs from the local application's logs.
			var stdout io.ReadCloser
			var stderr io.ReadCloser
			if Verbose {
				var err error
				stdout, err = cmd.StdoutPipe()
				if err != nil {
					return fmt.Errorf("failed to create stdout pipe: %v", err)
				}
				defer stdout.Close()

				stderr, err = cmd.StderrPipe()
				if err != nil {
					return fmt.Errorf("failed to create stderr pipe: %v", err)
				}
				defer stderr.Close()
			} else {
				cmd.Stdout = os.Stdout
				cmd.Stderr = os.Stderr
			}

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

						err := invoke(ctx, httpClient, bridgeSessionURL, requestID, res)
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

			err := cmd.Start()
			if err != nil {
				return fmt.Errorf("failed to start %s: %v", strings.Join(args, " "), err)
			}

			if Verbose {
				prefix := []byte(pad(arg0, prefixWidth) + " | ")
				suffix := []byte("\n")
				if Color {
					prefix = []byte("\033[35m" + pad(arg0, prefixWidth) + " \033[90m|\033[0m ")
				}
				go printPrefixedLines(os.Stderr, stdout, prefix, suffix)
				go printPrefixedLines(os.Stderr, stderr, prefix, suffix)
			}

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
					dialog("To resume this Dispatch session:\n\n\tdispatch run --session %s -- %s",
						BridgeSession, strings.Join(args, " "))
				}
			}

			if err != nil {
				return fmt.Errorf("failed to invoke command '%s': %v", strings.Join(args, " "), err)
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&BridgeSession, "session", "s", "", "Optional session to resume")
	cmd.Flags().StringVarP(&LocalEndpoint, "endpoint", "e", defaultEndpoint, "Host:port that the local application endpoint is listening on")
	cmd.Flags().BoolVarP(&Verbose, "verbose", "", false, "Enable verbose logging")

	return cmd
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

func invoke(ctx context.Context, client *http.Client, url, requestID string, bridgeGetRes *http.Response) error {
	slog.Debug("sending request to local application", "endpoint", LocalEndpoint, "request_id", requestID)

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
	switch d := runRequest.Directive.(type) {
	case *sdkv1.RunRequest_Input:
		slog.Debug("calling function", "function", runRequest.Function, "request_id", requestID)
	case *sdkv1.RunRequest_PollResult:
		slog.Debug("resuming function", "function", runRequest.Function, "call_results", len(d.PollResult.Results), "request_id", requestID)
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
		return fmt.Errorf("failed to contact local application endpoint (%s): %v. Please check that -e,--endpoint is correct.", LocalEndpoint, err)
	}

	// Buffer the response body in memory.
	endpointResBody := &bytes.Buffer{}
	if endpointRes.ContentLength > 0 {
		endpointResBody.Grow(int(endpointRes.ContentLength))
	}
	endpointResBody.ContetnLength, err = io.Copy(endpointResBody, endpointRes.Body)
	endpointRes.Body.Close()
	if err != nil {
		return fmt.Errorf("failed to read response from local application endpoint (%s): %v", LocalEndpoint, err)
	}
	endpointRes.Body = io.NopCloser(endpointResBody)

	// Parse the response body from the API.
	if endpointRes.StatusCode == http.StatusOK && endpointRes.Header.Get("Content-Type") == "application/proto" {
		var runResponse sdkv1.RunResponse
		if err := proto.Unmarshal(endpointResBody.Bytes(), &runResponse); err != nil {
			return fmt.Errorf("invalid response from local application endpoint (%s): %v", LocalEndpoint, err)
		}
		switch runResponse.Status {
		case sdkv1.Status_STATUS_OK:
			switch d := runResponse.Directive.(type) {
			case *sdkv1.RunResponse_Exit:
				if d.Exit.TailCall != nil {
					slog.Debug("function tail-called", "function", runRequest.Function, "tail_call", d.Exit.TailCall.Function, "request_id", requestID)
				} else {
					slog.Debug("function call succeeded", "function", runRequest.Function, "request_id", requestID)
				}
			case *sdkv1.RunResponse_Poll:
				slog.Debug("function yielded", "function", runRequest.Function, "calls", len(d.Poll.Calls), "request_id", requestID)
			}
		default:
			slog.Debug("function call failed", "function", runRequest.Function, "status", runResponse.Status, "request_id", requestID)
		}
	} else {
		// The response might indicate some other issue, e.g. it could be a 404 if the function can't be found
		slog.Debug("function call failed", "function", runRequest.Function, "http_status", endpointRes.StatusCode, "request_id", requestID)
	}

	// Use io.Pipe to convert the response writer into an io.Reader.
	pr, pw := io.Pipe()
	go func() {
		err := endpointRes.Write(pw)
		pw.CloseWithError(err)
	}()

	slog.Debug("sending response to Dispatch", "request_id", requestID)

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
		return fmt.Errorf("failed to contact Dispatch API or write response: %v", err)
	}
	if bridgePostRes.StatusCode != http.StatusAccepted {
		return fmt.Errorf("failed to contact Dispatch API: response code %d", bridgePostRes.StatusCode)
	}
	return nil
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
		return fmt.Errorf("failed to contact Dispatch API: %v", err)
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to contact Dispatch API: response code %d", res.StatusCode)
	}
	return nil
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

type prefixHandler struct {
	slog.Handler
	stream io.Writer
	prefix []byte
	suffix []byte
}

func (h *prefixHandler) Handle(ctx context.Context, r slog.Record) error {
	if _, err := h.stream.Write(h.prefix); err != nil {
		return err
	}
	if err := h.Handler.Handle(ctx, r); err != nil {
		return err
	}
	_, err := h.stream.Write(h.suffix)
	return err
}

func printPrefixedLines(w io.Writer, r io.Reader, prefix, suffix []byte) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		_, _ = w.Write(prefix)
		_, _ = w.Write(scanner.Bytes())
		_, _ = w.Write(suffix)
	}
}

func pad(s string, width int) string {
	if len(s) < width {
		return s + strings.Repeat(" ", width-len(s))
	}
	return s
}
