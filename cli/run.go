package cli

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var (
	BridgeSession string
	LocalEndpoint string
)

const defaultEndpoint = "localhost:8000"

const pollTimeout = 30 * time.Second

var httpClient = &http.Client{
	Transport: http.DefaultTransport,
	Timeout:   pollTimeout,
}

func runCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run a Dispatch application",
		Long: fmt.Sprintf(`Run a Dispatch application.

The command to start the local application should be specified
after the run command and its options:

  dispatch run [options] -- <command>

Dispatch spawns the local application and then dispatches function
calls to it continuously.

Dispatch connects to the local application on http://%s.
If the local application is listening on a different host or port,
please set the --endpoint option appropriately.

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
			if BridgeSession == "" {
				BridgeSession = uuid.New().String()
			}

			dialog(`Starting Dispatch session: %v

Run 'dispatch help run' to learn more about Dispatch session.`, BridgeSession)

			// Execute the command, forwarding the environment and
			// setting the necessary extra DISPATCH_* variables.
			cmd := exec.Command(args[0], args[1:]...)

			cmd.Stdin = os.Stdin
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr

			cmd.Env = append(
				os.Environ(),
				"DISPATCH_API_KEY="+DispatchApiKey,
				"DISPATCH_ENDPOINT_URL=bridge://"+BridgeSession,
			)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			// Setup signal handler.
			signals := make(chan os.Signal, 2)
			signal.Notify(signals, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM)
			var signaled bool
			go func() {
				for {
					select {
					case <-ctx.Done():
						return
					case <-signals:
						cancel()
						if !signaled {
							signaled = true
							_ = cmd.Process.Signal(syscall.SIGTERM)
						} else {
							_ = cmd.Process.Kill()
						}
					}
				}
			}()

			// Poll for work in the background.
			var successfulPolls int64
			go func() {
				for {
					if err := poll(ctx, httpClient); err != nil {
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
					}
					atomic.AddInt64(&successfulPolls, +1)
				}
			}()

			err := cmd.Run()

			// If the command was halted by a signal rather than some other error,
			// assume that the command invocation succeeded and that the user may
			// want to resume this session.
			if signaled {
				err = nil

				if atomic.LoadInt64(&successfulPolls) > 0 {
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
	cmd.Flags().StringVarP(&LocalEndpoint, "endpoint", "e", defaultEndpoint, "Host:port that the local application is listening on")

	return cmd
}

func poll(ctx context.Context, client *http.Client) error {
	// Fetch a request from the API.
	bridgeSessionURL := fmt.Sprintf("%s/sessions/%s", DispatchBridgeUrl, BridgeSession)
	slog.Debug("getting request from API", "url", bridgeSessionURL)

	bridgeGetReq, err := http.NewRequestWithContext(ctx, "GET", bridgeSessionURL, nil)
	if err != nil {
		panic(err)
	}
	bridgeGetReq.Header.Add("Authorization", "Bearer "+DispatchApiKey)
	bridgeGetReq.Header.Add("Connect-Timeout-Ms", strconv.FormatInt(pollTimeout.Milliseconds(), 10))
	if DispatchBridgeHostHeader != "" {
		bridgeGetReq.Host = DispatchBridgeHostHeader
	}

	bridgeGetRes, err := client.Do(bridgeGetReq)
	if err != nil {
		return fmt.Errorf("failed to contact Dispatch API (%s): %v", DispatchBridgeUrl, err)
	}
	if bridgeGetRes.StatusCode != http.StatusOK {
		bridgeGetRes.Body.Close()

		switch bridgeGetRes.StatusCode {
		case http.StatusUnauthorized:
			return authError{}
		case http.StatusGatewayTimeout:
			// A 504 is expected when long polling and no requests
			// are available. Return a nil in this case and let the
			// caller try again.
			return nil
		default:
			return fmt.Errorf("failed to contact Dispatch API (%s): response code %d", DispatchBridgeUrl, bridgeGetRes.StatusCode)
		}
	}

	requestID := bridgeGetRes.Header.Get("X-Request-Id")

	go func() {
		if err := invoke(ctx, client, bridgeSessionURL, requestID, bridgeGetRes); err != nil {
			slog.Warn(err.Error())
		}
	}()

	return nil
}

func invoke(ctx context.Context, client *http.Client, bridgeSessionURL, requestID string, bridgeGetRes *http.Response) error {
	defer bridgeGetRes.Body.Close()

	slog.Debug("sending request from Dispatch API to local application", "endpoint", LocalEndpoint, "request_id", requestID)

	// Extract the nested header/body.
	endpointReq, err := http.ReadRequest(bufio.NewReader(bridgeGetRes.Body))
	if err != nil {
		return fmt.Errorf("invalid response from Dispatch API: %v", err)
	}
	endpointReq = endpointReq.WithContext(ctx)

	// The RequestURI field must be cleared for client.Do() to
	// accept the request below.
	endpointReq.RequestURI = ""

	// Forward the request to the local application.
	endpointReq.Host = LocalEndpoint
	endpointReq.URL.Scheme = "http"
	endpointReq.URL.Host = LocalEndpoint
	endpointRes, err := client.Do(endpointReq)
	if err != nil {
		return fmt.Errorf("failed to contact local application (%s): %v. Please check that -e,--endpoint is correct.", LocalEndpoint, err)
	}
	defer endpointRes.Body.Close()

	bridgeGetRes.Body.Close()

	// Buffer the response from the endpoint.
	// TODO: pipe it into the request below
	var bufferedEndpointRes bytes.Buffer
	if err := endpointRes.Write(&bufferedEndpointRes); err != nil {
		return fmt.Errorf("failed to read response from local application (%s): %v", LocalEndpoint, err)
	}
	endpointRes.Body.Close()

	slog.Debug("sending local application's response to Dispatch API", "request_id", requestID)

	// Send the response back to the API.
	bridgePostReq, err := http.NewRequestWithContext(ctx, "POST", bridgeSessionURL, bufio.NewReader(&bufferedEndpointRes))
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
		return fmt.Errorf("failed to contact Dispatch API: %v", err)
	}
	if bridgePostRes.StatusCode != 202 {
		return fmt.Errorf("failed to contact Dispatch API: response code %d", bridgePostRes.StatusCode)
	}
	return nil
}
